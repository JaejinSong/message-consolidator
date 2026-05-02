package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Why: Slack tolerates ±5min timestamp skew per docs (api.slack.com/authentication/verifying-requests-from-slack).
//      Tighter than 5min causes legitimate replays during clock drift; looser opens replay window.
const slackSignatureMaxAge = 5 * time.Minute

var (
	ErrSlackSignatureMissing  = errors.New("slack: signature header missing")
	ErrSlackTimestampMissing  = errors.New("slack: timestamp header missing")
	ErrSlackTimestampInvalid  = errors.New("slack: timestamp invalid")
	ErrSlackTimestampExpired  = errors.New("slack: timestamp outside accept window")
	ErrSlackSignatureMismatch = errors.New("slack: signature mismatch")
	ErrSlackSecretMissing     = errors.New("slack: signing secret not configured")
)

// VerifySlackRequest validates the v0 HMAC-SHA256 signature Slack attaches to every webhook request.
// Why: Slack signs `v0:<timestamp>:<raw body>` with the workspace signing secret; rejecting unsigned
//      or replayed requests is the only auth path for /api/slack/{events,interactive,commands}.
func VerifySlackRequest(secret, timestamp, signature string, body []byte) error {
	return verifySlackRequestAt(secret, timestamp, signature, body, time.Now())
}

func verifySlackRequestAt(secret, timestamp, signature string, body []byte, now time.Time) error {
	if secret == "" {
		return ErrSlackSecretMissing
	}
	if signature == "" {
		return ErrSlackSignatureMissing
	}
	if timestamp == "" {
		return ErrSlackTimestampMissing
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrSlackTimestampInvalid, err.Error())
	}
	delta := now.Unix() - ts
	if delta < 0 {
		delta = -delta
	}
	if time.Duration(delta)*time.Second > slackSignatureMaxAge {
		return ErrSlackTimestampExpired
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:"))
	mac.Write([]byte(timestamp))
	mac.Write([]byte(":"))
	mac.Write(body)
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	// Why: constant-time compare prevents timing oracles on the HMAC tag.
	if !hmac.Equal([]byte(expected), []byte(strings.TrimSpace(signature))) {
		return ErrSlackSignatureMismatch
	}
	return nil
}
