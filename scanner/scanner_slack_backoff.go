package scanner

import (
	"message-consolidator/logger"
	"strings"
	"sync"
	"time"
)

// channelBackoffWindow bounds how long a channel identified as inaccessible (bot
// removed, channel archived/deleted, missing scope) is skipped before retry.
const channelBackoffWindow = 59 * time.Minute

// slackAccessFailureReasons are the slack-go error strings that indicate the bot can
// no longer read a channel, as opposed to a transient/unknown failure worth retrying
// every cycle with a Warn log.
var slackAccessFailureReasons = []string{"channel_not_found", "not_in_channel", "is_archived", "missing_scope"}

func classifyAccessError(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	msg := err.Error()
	for _, reason := range slackAccessFailureReasons {
		if strings.Contains(msg, reason) {
			return reason, true
		}
	}
	return "", false
}

type inaccessibleChannelInfo struct {
	reason string
	until  time.Time
}

// inaccessibleChannelKey scopes the backoff to which client kind saw the failure, so a
// bot 'not_in_channel' never blocks a user-token fetch on the same channel (and vice
// versa).
type inaccessibleChannelKey struct {
	kind      string
	channelID string
}

var (
	inaccessibleMu       sync.Mutex
	inaccessibleChannels = map[inaccessibleChannelKey]inaccessibleChannelInfo{}
)

// recordChannelInaccessible remembers (kind, chID) as unreachable for channelBackoffWindow
// and logs exactly one Error line per channel per window (no per-thread spam).
func recordChannelInaccessible(kind, chID, reason string) {
	inaccessibleMu.Lock()
	defer inaccessibleMu.Unlock()
	key := inaccessibleChannelKey{kind: kind, channelID: chID}
	if info, ok := inaccessibleChannels[key]; ok && time.Now().Before(info.until) {
		return
	}
	inaccessibleChannels[key] = inaccessibleChannelInfo{reason: reason, until: time.Now().Add(channelBackoffWindow)}
	logger.Errorf("[SLACK] channel %s inaccessible for %s client (%s): not a member or channel gone", chID, kind, reason)
}

func isChannelInaccessible(kind, chID string) bool {
	inaccessibleMu.Lock()
	defer inaccessibleMu.Unlock()
	info, ok := inaccessibleChannels[inaccessibleChannelKey{kind: kind, channelID: chID}]
	return ok && time.Now().Before(info.until)
}

// InaccessibleSlackChannels exposes the current "kind:channelID"→reason backoff set for
// a future status endpoint. Entries past their backoff window are omitted.
func InaccessibleSlackChannels() map[string]string {
	inaccessibleMu.Lock()
	defer inaccessibleMu.Unlock()
	now := time.Now()
	out := make(map[string]string, len(inaccessibleChannels))
	for key, info := range inaccessibleChannels {
		if now.Before(info.until) {
			out[key.kind+":"+key.channelID] = info.reason
		}
	}
	return out
}
