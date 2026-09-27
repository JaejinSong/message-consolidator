package channels

import (
	"context"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/internal/whataphttpx"
	"net/url"

	"github.com/slack-go/slack"
)

// slackUserOAuthScopes are the Slack user-token (xoxp) scopes requested when a user links
// their own Slack account. Why: im:write/im:history/mpim:* are intentionally excluded —
// DMs stay out of scope so this integration never reads a user's private conversations.
const slackUserOAuthScopes = "channels:history,groups:history,channels:read,groups:read,users:read,users:read.email"

var (
	slackUserOAuthClientID     string
	slackUserOAuthClientSecret string
	slackUserOAuthRedirectURI  string
)

// SetupSlackUserOAuth wires the per-user Slack OAuth (xoxp) config, mirroring SetupGmailOAuth.
func SetupSlackUserOAuth(cfg *config.Config) {
	slackUserOAuthClientID = cfg.SlackClientID
	slackUserOAuthClientSecret = cfg.SlackClientSecret
	slackUserOAuthRedirectURI = fmt.Sprintf("%s/auth/slack/callback", cfg.AppBaseURL)
}

// SlackUserOAuthConfigured reports whether SLACK_CLIENT_ID/SECRET are set.
func SlackUserOAuthConfigured() bool {
	return slackUserOAuthClientID != "" && slackUserOAuthClientSecret != ""
}

// SlackUserAuthURL builds the Slack v2 user-token authorize URL for the given CSRF state.
func SlackUserAuthURL(state string) string {
	q := url.Values{
		"client_id":    {slackUserOAuthClientID},
		"user_scope":   {slackUserOAuthScopes},
		"redirect_uri": {slackUserOAuthRedirectURI},
		"state":        {state},
	}
	return "https://slack.com/oauth/v2/authorize?" + q.Encode()
}

// SlackUserExchange is the caller-relevant slice of Slack's oauth.v2.access response for
// the authed (installing) user's token grant.
type SlackUserExchange struct {
	AccessToken string
	SlackUserID string
	Scope       string
}

// ExchangeSlackUserCode swaps an OAuth code for the authed user's access token.
func ExchangeSlackUserCode(ctx context.Context, code string) (SlackUserExchange, error) {
	resp, err := slack.GetOAuthV2ResponseContext(ctx, whataphttpx.Client(), //nolint:contextcheck // whataphttpx.Client takes no ctx by design; trace rides on http.Request.Context
		slackUserOAuthClientID, slackUserOAuthClientSecret, code, slackUserOAuthRedirectURI)
	if err != nil {
		return SlackUserExchange{}, fmt.Errorf("slack oauth exchange: %w", err)
	}
	if resp.AuthedUser.AccessToken == "" {
		return SlackUserExchange{}, fmt.Errorf("slack oauth exchange: missing authed_user access token")
	}
	return SlackUserExchange{
		AccessToken: resp.AuthedUser.AccessToken,
		SlackUserID: resp.AuthedUser.ID,
		Scope:       resp.AuthedUser.Scope,
	}, nil
}

// VerifySlackUserEmail calls users.info with the user's own token to fetch the Slack
// profile email bound to slackUserID, so the caller can confirm it matches the app account.
func VerifySlackUserEmail(ctx context.Context, userToken, slackUserID string) (string, error) {
	api := slack.New(userToken, slack.OptionHTTPClient(whataphttpx.Client())) //nolint:contextcheck // whataphttpx.Client takes no ctx by design; trace rides on http.Request.Context
	user, err := api.GetUserInfoContext(ctx, slackUserID)
	if err != nil {
		return "", fmt.Errorf("slack users.info: %w", err)
	}
	return user.Profile.Email, nil
}

// RevokeSlackUserToken invalidates the user token at Slack. Why: deleting the local row
// alone leaves the xoxp grant live at Slack, so "disconnect" would not be authoritative.
func RevokeSlackUserToken(ctx context.Context, userToken string) error {
	api := slack.New(userToken, slack.OptionHTTPClient(whataphttpx.Client())) //nolint:contextcheck // whataphttpx.Client takes no ctx by design; trace rides on http.Request.Context
	if _, err := api.SendAuthRevokeContext(ctx, userToken); err != nil {
		return fmt.Errorf("slack auth.revoke: %w", err)
	}
	return nil
}
