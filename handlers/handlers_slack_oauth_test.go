package handlers

import (
	"context"
	"errors"
	"message-consolidator/channels"
	"message-consolidator/config"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// withSlackOAuthConfig points the package-level Slack OAuth config at fake values and
// restores it on cleanup, mirroring withFakeTokenEndpoint in channels/gmail_oauth_test.go.
func withSlackOAuthConfig(t *testing.T, cfg *config.Config) {
	t.Helper()
	channels.SetupSlackUserOAuth(cfg)
	t.Cleanup(func() {
		channels.SetupSlackUserOAuth(&config.Config{})
	})
}

// withFakeSlackExchange overrides the exchange/verify seams for the duration of the test.
func withFakeSlackExchange(t *testing.T,
	exchange func(ctx context.Context, code string) (channels.SlackUserExchange, error),
	verify func(ctx context.Context, token, slackUserID string) (string, error),
) {
	t.Helper()
	origExchange := exchangeSlackUserCodeFunc
	origVerify := verifySlackUserEmailFunc
	exchangeSlackUserCodeFunc = exchange
	verifySlackUserEmailFunc = verify
	t.Cleanup(func() {
		exchangeSlackUserCodeFunc = origExchange
		verifySlackUserEmailFunc = origVerify
	})
}

func TestHandleSlackConnect_NotConfigured(t *testing.T) {
	withSlackOAuthConfig(t, &config.Config{})

	api := &API{Config: &config.Config{}}
	req := NewMockRequest("GET", "/auth/slack/connect", "user@example.com")
	rr := httptest.NewRecorder()

	api.HandleSlackConnect(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleSlackConnect_RedirectsWithUserScopeAndNonce(t *testing.T) {
	withSlackOAuthConfig(t, &config.Config{
		SlackClientID:     "client-123",
		SlackClientSecret: "secret-abc",
		AppBaseURL:        "https://app.example.com",
	})

	api := &API{Config: &config.Config{}}
	req := NewMockRequest("GET", "/auth/slack/connect", "user@example.com")
	rr := httptest.NewRecorder()

	api.HandleSlackConnect(rr, req)

	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusTemporaryRedirect)
	}
	loc := rr.Header().Get("Location")
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("invalid redirect location %q: %v", loc, err)
	}
	if parsed.Host != "slack.com" {
		t.Errorf("redirect host = %q, want slack.com", parsed.Host)
	}
	q := parsed.Query()
	if q.Get("user_scope") == "" {
		t.Error("expected user_scope in redirect URL")
	}
	if got := q.Get("redirect_uri"); got != "https://app.example.com/auth/slack/callback" {
		t.Errorf("redirect_uri = %q, want callback URL", got)
	}
	state := q.Get("state")
	if state == "" {
		t.Fatal("expected non-empty state nonce")
	}

	// Why: the nonce must be redeemable exactly once against the email that requested it.
	email, ok := consumeSlackOAuthNonce(state)
	if !ok || email != "user@example.com" {
		t.Errorf("consumeSlackOAuthNonce = (%q, %v), want (user@example.com, true)", email, ok)
	}
}

func TestHandleSlackCallback_UnknownOrExpiredNonce(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	api := &API{Config: &config.Config{}}
	req := httptest.NewRequest("GET", "/auth/slack/callback?state=does-not-exist&code=abc", nil)
	rr := httptest.NewRecorder()

	api.HandleSlackCallback(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleSlackCallback_ReusedNonceRejected(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	withFakeSlackExchange(t,
		func(ctx context.Context, code string) (channels.SlackUserExchange, error) {
			return channels.SlackUserExchange{AccessToken: "xoxp-tok", SlackUserID: "U1", Scope: "channels:read"}, nil
		},
		func(ctx context.Context, token, slackUserID string) (string, error) {
			return "user@example.com", nil
		},
	)

	nonce, err := issueSlackOAuthNonce("user@example.com")
	if err != nil {
		t.Fatalf("issueSlackOAuthNonce: %v", err)
	}

	api := &API{Config: &config.Config{}}

	// First use succeeds.
	req1 := httptest.NewRequest("GET", "/auth/slack/callback?state="+nonce+"&code=abc", nil)
	rr1 := httptest.NewRecorder()
	api.HandleSlackCallback(rr1, req1)
	if rr1.Code != http.StatusTemporaryRedirect {
		t.Fatalf("first callback status = %d, want %d", rr1.Code, http.StatusTemporaryRedirect)
	}

	// Reuse must fail.
	req2 := httptest.NewRequest("GET", "/auth/slack/callback?state="+nonce+"&code=abc", nil)
	rr2 := httptest.NewRecorder()
	api.HandleSlackCallback(rr2, req2)
	if rr2.Code != http.StatusBadRequest {
		t.Fatalf("reused nonce status = %d, want %d", rr2.Code, http.StatusBadRequest)
	}
}

func TestHandleSlackCallback_Denied(t *testing.T) {
	api := &API{Config: &config.Config{}}
	req := httptest.NewRequest("GET", "/auth/slack/callback?error=access_denied", nil)
	rr := httptest.NewRecorder()

	api.HandleSlackCallback(rr, req)

	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusTemporaryRedirect)
	}
	if loc := rr.Header().Get("Location"); loc != "/?slack=denied" {
		t.Errorf("Location = %q, want /?slack=denied", loc)
	}
}

func TestHandleSlackCallback_EmailMismatchNotSaved(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	requester := "app-user@example.com"
	withFakeSlackExchange(t,
		func(ctx context.Context, code string) (channels.SlackUserExchange, error) {
			return channels.SlackUserExchange{AccessToken: "xoxp-tok", SlackUserID: "U1", Scope: "channels:read"}, nil
		},
		func(ctx context.Context, token, slackUserID string) (string, error) {
			return "someone-else@example.com", nil
		},
	)

	nonce, err := issueSlackOAuthNonce(requester)
	if err != nil {
		t.Fatalf("issueSlackOAuthNonce: %v", err)
	}

	api := &API{Config: &config.Config{}}
	req := httptest.NewRequest("GET", "/auth/slack/callback?state="+nonce+"&code=abc", nil)
	rr := httptest.NewRecorder()

	api.HandleSlackCallback(rr, req)

	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusTemporaryRedirect)
	}
	if loc := rr.Header().Get("Location"); loc != "/?slack=mismatch" {
		t.Errorf("Location = %q, want /?slack=mismatch", loc)
	}
	if store.HasSlackUserToken(requester) {
		t.Error("token must not be saved on email mismatch")
	}
}

func TestHandleSlackCallback_Success(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "connect-success@example.com"
	withFakeSlackExchange(t,
		func(ctx context.Context, code string) (channels.SlackUserExchange, error) {
			return channels.SlackUserExchange{AccessToken: "xoxp-tok", SlackUserID: "U42", Scope: "channels:read,users:read"}, nil
		},
		func(ctx context.Context, token, slackUserID string) (string, error) {
			return strings.ToUpper(email), nil // Why: exercises the case-insensitive email compare.
		},
	)

	nonce, err := issueSlackOAuthNonce(email)
	if err != nil {
		t.Fatalf("issueSlackOAuthNonce: %v", err)
	}

	api := &API{Config: &config.Config{}}
	req := httptest.NewRequest("GET", "/auth/slack/callback?state="+nonce+"&code=abc", nil)
	rr := httptest.NewRecorder()

	api.HandleSlackCallback(rr, req)

	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusTemporaryRedirect)
	}
	if loc := rr.Header().Get("Location"); loc != "/?slack=connected" {
		t.Errorf("Location = %q, want /?slack=connected", loc)
	}

	got, ok, err := store.GetSlackUserToken(context.Background(), email)
	if err != nil {
		t.Fatalf("GetSlackUserToken: %v", err)
	}
	if !ok {
		t.Fatal("expected token to be saved")
	}
	if got.Token != "xoxp-tok" || got.SlackUserID != "U42" {
		t.Errorf("saved token = %+v, want AccessToken=xoxp-tok SlackUserID=U42", got)
	}
}

func TestHandleSlackCallback_ExchangeFailure(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "exchange-fail@example.com"
	withFakeSlackExchange(t,
		func(ctx context.Context, code string) (channels.SlackUserExchange, error) {
			return channels.SlackUserExchange{}, errors.New("boom")
		},
		func(ctx context.Context, token, slackUserID string) (string, error) {
			return email, nil
		},
	)

	nonce, err := issueSlackOAuthNonce(email)
	if err != nil {
		t.Fatalf("issueSlackOAuthNonce: %v", err)
	}

	api := &API{Config: &config.Config{}}
	req := httptest.NewRequest("GET", "/auth/slack/callback?state="+nonce+"&code=abc", nil)
	rr := httptest.NewRecorder()

	api.HandleSlackCallback(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestConsumeSlackOAuthNonce_Expired(t *testing.T) {
	nonce, err := issueSlackOAuthNonce("expired@example.com")
	if err != nil {
		t.Fatalf("issueSlackOAuthNonce: %v", err)
	}

	// Why: force-expire the entry directly rather than sleeping 13 minutes in a test.
	slackOAuthNonceMu.Lock()
	entry := slackOAuthNonces[nonce]
	entry.expiry = entry.expiry.Add(-2 * slackOAuthNonceTTL)
	slackOAuthNonces[nonce] = entry
	slackOAuthNonceMu.Unlock()

	if _, ok := consumeSlackOAuthNonce(nonce); ok {
		t.Error("expected expired nonce to be rejected")
	}
}

func TestHandleSlackUserDisconnect(t *testing.T) {
	for _, tc := range []struct {
		name      string
		revokeErr error
	}{
		{"revoke succeeds", nil},
		{"revoke fails but local token is still removed", errors.New("slack down")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
			if err != nil {
				t.Fatalf("setup db: %v", err)
			}
			defer cleanup()

			var revoked string
			orig := revokeSlackUserTokenFunc
			revokeSlackUserTokenFunc = func(_ context.Context, token string) error {
				revoked = token
				return tc.revokeErr
			}
			defer func() { revokeSlackUserTokenFunc = orig }()

			email := "disconnect-me@example.com"
			if err := store.SaveSlackUserToken(context.Background(), email, store.SlackUserToken{
				Token: "xoxp-del", SlackUserID: "U9",
			}); err != nil {
				t.Fatalf("SaveSlackUserToken: %v", err)
			}

			api := &API{Config: &config.Config{}}
			rr := httptest.NewRecorder()
			api.HandleSlackUserDisconnect(rr, NewMockRequest("POST", "/api/slack/disconnect", email))

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
			}
			if revoked != "xoxp-del" {
				t.Errorf("revoked token = %q, want xoxp-del", revoked)
			}
			if store.HasSlackUserToken(email) {
				t.Error("expected token to be deleted")
			}
		})
	}
}

func TestBuildSlackStatus_UserTokenFields(t *testing.T) {
	got := buildSlackStatus(true, "U1", "", time.Now(), true, "U99")
	if !got.UserToken {
		t.Error("expected UserToken=true")
	}
	if got.UserTokenSlackID != "U99" {
		t.Errorf("UserTokenSlackID = %q, want U99", got.UserTokenSlackID)
	}
}
