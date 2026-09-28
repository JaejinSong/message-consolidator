package scanner

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"sync"
	"time"
)

// markSlackScanSuccess stamps the wall-clock time of the last clean scan pass, mirroring
// channels.markGmailScanSuccess. Why: /slack/status must be able to tell a live scan loop
// from a silently dead one (bot removed from channels, channel_not_found, etc). A user
// with any held cursor (tracker) is skipped -- their scan was not actually clean even
// though the channel fetch itself succeeded.
func markSlackScanSuccess(users []store.User, tracker *slackHeldTracker) {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	for _, u := range users {
		if tracker.hasHeld(u.Email) {
			logger.Warnf("[SLACK] withholding last_success stamp for %s: cursor(s) held", u.Email)
			continue
		}
		if err := store.UpdateLastScan(u.Email, store.SourceSlack, store.ScanTargetLastSuccess, ts); err != nil {
			logger.Warnf("[SLACK] record last_success failed for %s: %v", u.Email, err)
		}
	}
}

// slackHeldTracker collects the (email, channelID) pairs whose AI analysis did not
// finish this scan pass (AckScanned ok=false), so applySlackScanResults can withhold
// their cursor advance instead of skipping past the unanalyzed backlog.
type slackHeldTracker struct {
	mu   sync.Mutex
	held map[string]map[string]bool // email -> channelID -> held
}

func newSlackHeldTracker() *slackHeldTracker {
	return &slackHeldTracker{held: make(map[string]map[string]bool)}
}

func (t *slackHeldTracker) hold(email, channelID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.held[email] == nil {
		t.held[email] = make(map[string]bool)
	}
	t.held[email][channelID] = true
}

func (t *slackHeldTracker) isHeld(email, channelID string) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.held[email][channelID]
}

// hasHeld reports whether email has any held channel this pass.
func (t *slackHeldTracker) hasHeld(email string) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.held[email]) > 0
}

// applySlackScanResults commits newTS to store.UpdateLastScan, withholding any
// (email, channelID) tracker marked held and -- per the incident fix -- the entire
// pass when ctx is already done: a scan that timed out mid-analysis must not advance
// past whatever channels happened to fetch before the deadline hit.
func applySlackScanResults(ctx context.Context, newTS map[string]map[string]string, tracker *slackHeldTracker) {
	if err := ctx.Err(); err != nil {
		logger.Warnf("[SLACK] ctx done (%v); holding all cursors this pass", err)
		return
	}
	for email, channelMap := range newTS {
		for chanID := range channelMap {
			if tracker.isHeld(email, chanID) {
				logger.Warnf("[SLACK] holding cursor for %s/%s: analysis incomplete", email, chanID)
				delete(channelMap, chanID)
			}
		}
	}
	updateSlackCursors(newTS)
}

func updateChannelCursor(newTS map[string]map[string]string, email, channelID, msgID string) {
	if newTS[email] == nil {
		newTS[email] = make(map[string]string)
	}
	if curr, ok := newTS[email][channelID]; !ok || msgID > curr {
		newTS[email][channelID] = msgID
	}
}

func updateSlackCursors(newTS map[string]map[string]string) {
	for email, channelMap := range newTS {
		for chanID, ts := range channelMap {
			if err := store.UpdateLastScan(email, store.SourceSlack, chanID, ts); err != nil {
				logger.Warnf("[SCAN] slack: UpdateLastScan failed for %s/%s: %v", email, chanID, err)
			}
		}
	}
}
