package channels

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/internal/whataphttpx"
	"message-consolidator/store"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

const (
	CategorySent   = "발신 메일" //Why: Identifies emails sent by the user to determine if they constitute a commitment or a task update.
	CategoryMine   = "내 업무"  //Why: Marks emails where the user is the primary recipient as personal tasks.
	CategoryOthers = "기타 업무" //Why: Classifies CC'd or group emails as lower-priority informational items.
)

var GmailOAuthConfig *oauth2.Config

func SetupGmailOAuth(cfg *config.Config) {
	GmailOAuthConfig = &oauth2.Config{
		RedirectURL:  fmt.Sprintf("%s/auth/gmail/callback", cfg.AppBaseURL),
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		Scopes: []string{
			"https://www.googleapis.com/auth/gmail.readonly",
			"https://www.googleapis.com/auth/gmail.send",
		},
		Endpoint: google.Endpoint,
	}
}

func GetGmailAuthURL(state string) string {
	return GmailOAuthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
}

func ExchangeGmailCode(ctx context.Context, code string) (*oauth2.Token, error) {
	return GmailOAuthConfig.Exchange(ctx, code)
}

func GetGmailService(ctx context.Context, email string) (*gmail.Service, error) {
	tokenJSON, err := store.GetGmailToken(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("no gmail token for %s: %w", email, err)
	}

	var token oauth2.Token
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil {
		return nil, fmt.Errorf("failed to parse gmail token for %s: %w", email, err)
	}

	tokenSource := GmailOAuthConfig.TokenSource(ctx, &token)

	//Why: Automatically refreshes the OAuth2 token if it has expired and persists the new token to the database to ensure uninterrupted Gmail access.
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh gmail token for %s: %w", email, err)
	}
	if newToken.AccessToken != token.AccessToken {
		newTokenJSON, _ := json.Marshal(newToken)
		_ = store.SaveGmailToken(ctx, email, string(newTokenJSON))
	}

	// Why: oauth2.NewClient builds an http.Client that auto-injects bearer tokens
	// from tokenSource. WrapClient layers WhaTap's RoundTripper on top of that
	// transport so every Gmail API call (Users.Messages.List, Users.Messages.Get,
	// thread fetches, ...) appears as an HTTPC step under the parent TX with
	// auth still attached. Passing option.WithHTTPClient forces the SDK to use
	// our wrapped client instead of building its own.
	httpClient := whataphttpx.WrapClient(oauth2.NewClient(ctx, tokenSource)) //nolint:contextcheck // WrapClient builds a transport; ctx is propagated by oauth2.NewClient and per-request SDK calls.
	svc, err := gmail.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("failed to create gmail service: %w", err)
	}
	return svc, nil
}

// SendGmailEmail sends a plain-text email via the Gmail API using the stored OAuth token for `from`.
func SendGmailEmail(ctx context.Context, from, to, subject, body string) error {
	svc, err := GetGmailService(ctx, from)
	if err != nil {
		return fmt.Errorf("gmail send: get service: %w", err)
	}
	raw := buildRawMessage(from, to, subject, body)
	if _, err := svc.Users.Messages.Send(from, &gmail.Message{Raw: raw}).Context(ctx).Do(); err != nil {
		return fmt.Errorf("gmail send: %w", err)
	}
	return nil
}

// SendGmailEmailWithOrigin sends a weekly-report email tagged with X-WhatAp-Origin
// so the scanner's isSystemOriginEmail filter can skip re-ingestion on the next cycle.
func SendGmailEmailWithOrigin(ctx context.Context, from, to, subject, body string) (string, error) {
	svc, err := GetGmailService(ctx, from)
	if err != nil {
		return "", fmt.Errorf("gmail send: get service: %w", err)
	}
	raw := buildRawMessageWithHeaders(from, to, subject, body, map[string]string{"X-WhatAp-Origin": "weekly-report"})
	sent, err := svc.Users.Messages.Send(from, &gmail.Message{Raw: raw}).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("gmail send: %w", err)
	}
	return sent.Id, nil
}

func buildRawMessage(from, to, subject, body string) string {
	return buildRawMessageWithHeaders(from, to, subject, body, nil)
}

func buildRawMessageWithHeaders(from, to, subject, body string, extraHeaders map[string]string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/html; charset=UTF-8\r\n",
		from, to, subject))
	for k, v := range extraHeaders {
		sb.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return base64.RawURLEncoding.EncodeToString([]byte(sb.String()))
}
