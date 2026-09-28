package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"sync"
)

// Why: per-user Slack OAuth grants each need their own SlackClient (own users.list /
// AuthTest identity), cached by a hash of the token rather than the plaintext so a raw
// token never sits in a map key visible via a debugger or accidental log of map keys.
var (
	userSlackClientMu    sync.Mutex
	userSlackClientCache = map[string]*channels.SlackClient{}
)

func hashSlackToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func getOrInitUserSlackClient(token string) *channels.SlackClient {
	key := hashSlackToken(token)
	userSlackClientMu.Lock()
	defer userSlackClientMu.Unlock()
	if c, ok := userSlackClientCache[key]; ok {
		return c
	}
	c := channels.NewSlackClient(token)
	userSlackClientCache[key] = c
	return c
}

// slackTokenRevokedReasons are slack-go error strings that mean a per-user OAuth grant
// is dead and should be dropped rather than retried next cycle.
var slackTokenRevokedReasons = []string{"invalid_auth", "token_revoked", "account_inactive", "not_authed"}

func isSlackTokenRevoked(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, reason := range slackTokenRevokedReasons {
		if strings.Contains(msg, reason) {
			return true
		}
	}
	return false
}

// slackClientKindBot/slackClientKindUser tag which Slack identity a fetch used, so the
// thread sweeper's channel-inaccessible backoff (scanner_slack_threads.go) can key on
// (kind, channelID) instead of channelID alone.
const (
	slackClientKindBot  = "bot"
	slackClientKindUser = "user"
)

// slackClientKindForEmail reports which client kind will scan/fetch for email, without
// constructing a client. Why: the thread sweeper's per-channel backoff must key on this
// so a bot 'not_in_channel' failure never blocks a user-token fetch on the same channel.
func slackClientKindForEmail(email string) string {
	if store.HasSlackUserToken(email) {
		return slackClientKindUser
	}
	return slackClientKindBot
}

// clientForSlackUser returns email's own Slack client when a user token is on file,
// else falls back to the bot client sc.
func clientForSlackUser(ctx context.Context, email string, sc *channels.SlackClient) *channels.SlackClient {
	tok, ok, err := store.GetSlackUserToken(ctx, email)
	if err != nil || !ok {
		return sc
	}
	return getOrInitUserSlackClient(tok.Token) //nolint:contextcheck // SlackClient constructor; per-request ctx flows through individual API calls.
}

func splitSlackUsersByToken(users []store.User) (tokenUsers, botUsers []store.User) {
	for _, u := range users {
		if store.HasSlackUserToken(u.Email) {
			tokenUsers = append(tokenUsers, u)
			continue
		}
		botUsers = append(botUsers, u)
	}
	return tokenUsers, botUsers
}

// scanSlackForTokenUser scans Slack using u's own OAuth grant: LookupChannels/
// GetMessages only see u's own channel memberships. Cursors reuse the same store keys
// as the bot path, so a user switching between token and bot scanning resumes from
// wherever the last pass (either kind) left off.
func scanSlackForTokenUser(ctx context.Context, u store.User, wg *sync.WaitGroup) {
	tok, ok, err := store.GetSlackUserToken(ctx, u.Email)
	if err != nil || !ok {
		logger.Warnf("[SLACK] user-token lookup failed for %s: %v", u.Email, err)
		return
	}
	sc := getOrInitUserSlackClient(tok.Token) //nolint:contextcheck // SlackClient constructor; per-request ctx flows through individual API calls.

	chans, _, err := sc.LookupChannels()
	if err != nil {
		if !handleSlackTokenFailure(ctx, u.Email, err) {
			logger.Warnf("[SLACK] user-token channel fetch failed for %s: %v", u.Email, err)
		}
		return
	}
	if len(chans) == 0 {
		return
	}

	users := []store.User{u}
	userAl := prepareSlackUserAliases(ctx, users)
	candidates, newTS, fetchOK, fetchErr := collectSlackHistory(ctx, users, chans, sc, userAl)
	tracker := newSlackHeldTracker()
	processSlackCandidates(ctx, users, sc, candidates, wg, tracker)
	applySlackScanResults(ctx, newTS, tracker)
	if fetchOK && ctx.Err() == nil {
		markSlackScanSuccess(users, tracker)
		return
	}
	handleSlackTokenFailure(ctx, u.Email, fetchErr)
}

// handleSlackTokenFailure deletes email's Slack user token and logs the fallback
// decision when err indicates the OAuth grant itself is dead (see
// slackTokenRevokedReasons); other errors are left for the caller to log and retry
// next cycle. Returns true when the token was dropped.
func handleSlackTokenFailure(ctx context.Context, email string, err error) bool {
	if !isSlackTokenRevoked(err) {
		return false
	}
	logger.Warnf("[SLACK] user token rejected for %s (%s); falling back to the bot", email, err)
	_ = store.DeleteSlackUserToken(ctx, email)
	return true
}
