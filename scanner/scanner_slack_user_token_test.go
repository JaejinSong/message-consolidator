package scanner

import (
	"context"
	"errors"
	"testing"

	"message-consolidator/channels"
	"message-consolidator/store"
)

// TestSplitSlackUsersByToken_TokenUserExcludedFromBotPass verifies a user with a
// stored Slack OAuth grant lands in tokenUsers only -- never botUsers -- so a channel
// visible to both the token and the bot is never scanned twice for that user.
func TestSplitSlackUsersByToken_TokenUserExcludedFromBotPass(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	tokenUser := store.User{Email: "token-user@example.com"}
	botUser := store.User{Email: "bot-user@example.com"}
	if err := store.SaveSlackUserToken(ctx, tokenUser.Email, store.SlackUserToken{Token: "xoxp-fake"}); err != nil {
		t.Fatalf("SaveSlackUserToken: %v", err)
	}

	tokenUsers, botUsers := splitSlackUsersByToken([]store.User{tokenUser, botUser})

	if len(tokenUsers) != 1 || tokenUsers[0].Email != tokenUser.Email {
		t.Fatalf("tokenUsers = %+v, want only %s", tokenUsers, tokenUser.Email)
	}
	for _, u := range botUsers {
		if u.Email == tokenUser.Email {
			t.Fatalf("token user %s must not also appear in botUsers: %+v", tokenUser.Email, botUsers)
		}
	}
	if len(botUsers) != 1 || botUsers[0].Email != botUser.Email {
		t.Fatalf("botUsers = %+v, want only %s", botUsers, botUser.Email)
	}
}

// TestSplitSlackUsersByToken_AllBotWhenNoTokens is the bot-path-unchanged regression:
// when no user has a Slack OAuth grant, everyone lands in botUsers untouched.
func TestSplitSlackUsersByToken_AllBotWhenNoTokens(t *testing.T) {
	initTestDB(t)

	users := []store.User{{Email: "a@example.com"}, {Email: "b@example.com"}}
	tokenUsers, botUsers := splitSlackUsersByToken(users)

	if len(tokenUsers) != 0 {
		t.Fatalf("tokenUsers = %+v, want empty", tokenUsers)
	}
	if len(botUsers) != len(users) {
		t.Fatalf("botUsers = %+v, want all %d users", botUsers, len(users))
	}
}

// TestHandleSlackTokenFailure_RevokedDeletesToken covers the revoked-grant branch:
// the token is dropped and true is returned so the caller logs the fallback decision.
func TestHandleSlackTokenFailure_RevokedDeletesToken(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	email := "revoked@example.com"
	if err := store.SaveSlackUserToken(ctx, email, store.SlackUserToken{Token: "xoxp-fake"}); err != nil {
		t.Fatalf("SaveSlackUserToken: %v", err)
	}
	if !store.HasSlackUserToken(email) {
		t.Fatal("expected token to be present before failure handling")
	}

	dropped := handleSlackTokenFailure(ctx, email, errors.New("invalid_auth"))

	if !dropped {
		t.Error("handleSlackTokenFailure() = false, want true for invalid_auth")
	}
	if store.HasSlackUserToken(email) {
		t.Error("expected token to be deleted after invalid_auth")
	}
}

// TestHandleSlackTokenFailure_TransientErrorKeepsToken covers the non-revoked branch:
// a transient error (e.g. rate limiting) must not delete the stored token.
func TestHandleSlackTokenFailure_TransientErrorKeepsToken(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	email := "transient@example.com"
	if err := store.SaveSlackUserToken(ctx, email, store.SlackUserToken{Token: "xoxp-fake"}); err != nil {
		t.Fatalf("SaveSlackUserToken: %v", err)
	}

	dropped := handleSlackTokenFailure(ctx, email, errors.New("rate_limited"))

	if dropped {
		t.Error("handleSlackTokenFailure() = true, want false for a transient error")
	}
	if !store.HasSlackUserToken(email) {
		t.Error("expected token to survive a transient error")
	}
}

// TestIsSlackTokenRevoked covers every recognized revocation reason plus the
// pass-through case for an unrelated error.
func TestIsSlackTokenRevoked(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("invalid_auth"), true},
		{errors.New("token_revoked"), true},
		{errors.New("account_inactive"), true},
		{errors.New("not_authed"), true},
		{errors.New("rate_limited"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := isSlackTokenRevoked(c.err); got != c.want {
			t.Errorf("isSlackTokenRevoked(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// TestClientForSlackUser_PicksUserTokenClient verifies the sweep's client-selection
// seam: an email with a stored token gets its own client (distinct from the bot
// client passed in); an email without one falls back to the bot client unchanged.
func TestClientForSlackUser_PicksUserTokenClient(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	botClient := channels.NewSlackClient("xoxb-fake-bot")

	tokenEmail := "sweep-token-user@example.com"
	if err := store.SaveSlackUserToken(ctx, tokenEmail, store.SlackUserToken{Token: "xoxp-sweep-fake"}); err != nil {
		t.Fatalf("SaveSlackUserToken: %v", err)
	}

	got := clientForSlackUser(ctx, tokenEmail, botClient)
	if got == botClient {
		t.Error("expected a distinct client for a user with a stored token")
	}

	// Same token again must hit the cache and return the identical pointer.
	again := clientForSlackUser(ctx, tokenEmail, botClient)
	if again != got {
		t.Error("expected the cached user client to be reused across calls")
	}

	noTokenEmail := "sweep-bot-user@example.com"
	fallback := clientForSlackUser(ctx, noTokenEmail, botClient)
	if fallback != botClient {
		t.Error("expected the bot client fallback for a user with no stored token")
	}
}

// TestSlackClientKindForEmail mirrors clientForSlackUser's decision as a plain string,
// which the thread sweeper's channel backoff keys on.
func TestSlackClientKindForEmail(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	email := "kind-user@example.com"

	if got := slackClientKindForEmail(email); got != slackClientKindBot {
		t.Errorf("slackClientKindForEmail() = %q, want %q before any token exists", got, slackClientKindBot)
	}

	if err := store.SaveSlackUserToken(ctx, email, store.SlackUserToken{Token: "xoxp-kind-fake"}); err != nil {
		t.Fatalf("SaveSlackUserToken: %v", err)
	}
	if got := slackClientKindForEmail(email); got != slackClientKindUser {
		t.Errorf("slackClientKindForEmail() = %q, want %q once a token exists", got, slackClientKindUser)
	}
}

// TestChannelBackoff_KeyedPerClientKind verifies a bot-kind backoff entry never makes
// isChannelInaccessible report the same channel as blocked for the user kind, and
// vice versa -- a bot 'not_in_channel' must never block a user-token fetch.
func TestChannelBackoff_KeyedPerClientKind(t *testing.T) {
	resetInaccessibleChannels(t)

	recordChannelInaccessible(slackClientKindBot, "C_SHARED", "not_in_channel")

	if !isChannelInaccessible(slackClientKindBot, "C_SHARED") {
		t.Error("expected the bot kind to be marked inaccessible")
	}
	if isChannelInaccessible(slackClientKindUser, "C_SHARED") {
		t.Error("a bot-kind backoff must not block the user-kind fetch on the same channel")
	}

	recordChannelInaccessible(slackClientKindUser, "C_SHARED", "channel_not_found")
	if !isChannelInaccessible(slackClientKindUser, "C_SHARED") {
		t.Error("expected the user kind to be marked inaccessible after its own failure")
	}
}

// TestGetOrInitUserSlackClient_CachesByTokenHash verifies the same token returns the
// same client instance while a different token gets its own.
func TestGetOrInitUserSlackClient_CachesByTokenHash(t *testing.T) {
	a1 := getOrInitUserSlackClient("xoxp-cache-a")
	a2 := getOrInitUserSlackClient("xoxp-cache-a")
	b := getOrInitUserSlackClient("xoxp-cache-b")

	if a1 != a2 {
		t.Error("expected the same token to reuse the cached client")
	}
	if a1 == b {
		t.Error("expected a different token to get its own client")
	}
}
