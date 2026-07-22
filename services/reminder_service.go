package services

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"
)

// SlackPoster is the minimal Slack send-side dependency for reminders.
// Defined here (consumer side) per project interface convention.
type SlackPoster interface {
	SendDM(ctx context.Context, slackUserID, text string) error
}

// ReminderService dispatches deadline reminders via Slack DM.
type ReminderService struct {
	Slack          SlackPoster
	WindowsHours   []int // e.g. [24, 1] — each int generates a separate scan window
	TickToleranceM int   // ± minutes around each window center; default 10
}

func NewReminderService(slack SlackPoster, windowsHours []int) *ReminderService {
	if len(windowsHours) == 0 {
		windowsHours = []int{24, 1}
	}
	return &ReminderService{
		Slack:          slack,
		WindowsHours:   windowsHours,
		TickToleranceM: 10,
	}
}

// DispatchDueSoon scans each configured window once and sends DM for each
// due-soon message that has not yet been reminded for that window.
func (r *ReminderService) DispatchDueSoon(ctx context.Context) error {
	if r == nil || r.Slack == nil {
		return nil
	}
	now := time.Now().UTC()
	for _, h := range r.WindowsHours {
		if err := r.dispatchWindow(ctx, now, h); err != nil {
			logger.Warnf("[REMINDER] window %dh failed: %v", h, err)
			// continue other windows
		}
	}
	return nil
}

func (r *ReminderService) dispatchWindow(ctx context.Context, now time.Time, hours int) error {
	windowKey := windowKeyFor(hours)
	center := now.Add(time.Duration(hours) * time.Hour)
	half := time.Duration(r.TickToleranceM) * time.Minute
	start := center.Add(-half).Format(time.RFC3339)
	end := center.Add(half).Format(time.RFC3339)

	rows, err := store.SelectDueSoon(ctx, start, end)
	if err != nil {
		return fmt.Errorf("select due soon: %w", err)
	}

	for _, m := range rows {
		if store.HasReminded(m.Metadata, windowKey) {
			continue
		}
		user, err := store.GetOrCreateUser(ctx, m.UserEmail, "", "")
		if err != nil || user == nil || strings.TrimSpace(user.SlackID) == "" {
			continue
		}
		text := formatReminderText(m, hours)
		if err := r.Slack.SendDM(ctx, user.SlackID, text); err != nil {
			logger.Warnf("[REMINDER] SendDM failed user=%s msg=%d: %v", m.UserEmail, m.ID, err)
			continue // don't mark — retry next tick
		}
		if err := store.MarkReminded(ctx, m.UserEmail, m.ID, m.Metadata, windowKey, time.Now().UTC()); err != nil {
			logger.Warnf("[REMINDER] MarkReminded failed msg=%d: %v", m.ID, err)
		}
	}
	return nil
}

// windowKeyFor maps an hour count to a metadata key suffix.
// 24 → "24h", 1 → "1h", 48 → "48h" — keep it simple, no mapping table.
func windowKeyFor(hours int) string {
	return fmt.Sprintf("%dh", hours)
}

func formatReminderText(m store.DueSoonMessage, hours int) string {
	return fmt.Sprintf(":alarm_clock: 마감 %d시간 전 알림\n• 작업: %s\n• 마감: %s\n• 채널: %s/%s",
		hours, m.Task, m.Deadline, m.Source, m.Room)
}

// undatedWindowDays are the aging thresholds for commitments with no deadline.
var undatedWindowDays = []int{3, 7, 14}

// DispatchUndated sends Slack DMs for PROMISE/WAITING items that have no deadline
// and have aged past D+3, D+7, or D+14 since creation. Each window fires once per item.
func (r *ReminderService) DispatchUndated(ctx context.Context) error {
	if r == nil || r.Slack == nil {
		return nil
	}
	rows, err := store.SelectUndated(ctx)
	if err != nil {
		return fmt.Errorf("select undated: %w", err)
	}
	now := time.Now().UTC()
	for _, m := range rows {
		ageDays := int(now.Sub(m.CreatedAt).Hours() / 24)
		for _, threshold := range undatedWindowDays {
			if ageDays < threshold {
				continue
			}
			key := fmt.Sprintf("undated_d%d", threshold)
			if store.HasReminded(m.Metadata, key) {
				continue
			}
			user, err := store.GetOrCreateUser(ctx, m.UserEmail, "", "")
			if err != nil || user == nil || strings.TrimSpace(user.SlackID) == "" {
				break // no Slack ID — skip all windows for this user/item
			}
			text := formatUndatedNudgeText(m, ageDays)
			if err := r.Slack.SendDM(ctx, user.SlackID, text); err != nil {
				logger.Warnf("[REMINDER] undated SendDM failed user=%s msg=%d: %v", m.UserEmail, m.ID, err)
				break // don't mark; retry next tick
			}
			if err := store.MarkReminded(ctx, m.UserEmail, m.ID, m.Metadata, key, now); err != nil {
				logger.Warnf("[REMINDER] undated MarkReminded failed msg=%d: %v", m.ID, err)
			}
			break // one window per tick per item
		}
	}
	return nil
}

func formatUndatedNudgeText(m store.UndatedCommitment, ageDays int) string {
	emoji := ":hourglass_flowing_sand:"
	label := "약속"
	if m.Category == "WAITING" {
		emoji = ":eyes:"
		label = "대기 중인 항목"
	}
	return fmt.Sprintf("%s 기한 없는 %s (D+%d일)\n• 작업: %s\n• 채널: %s/%s",
		emoji, label, ageDays, m.Task, m.Source, m.Room)
}

// stalledReconfirmDays are the D+ thresholds (days since last update) at which a
// still-open TASK gets re-confirmed via a single Slack digest DM. Primes, ascending.
var stalledReconfirmDays = []int{13, 29}

// stalledReconfirmDigestCap bounds items per digest DM to keep messages readable.
const stalledReconfirmDigestCap = 7

// stalledReconfirmEntry pairs a stalled request with the reminder key to mark on send success.
type stalledReconfirmEntry struct {
	item store.StalledRequest
	key  string
}

// DispatchStalledReconfirm sends one Slack digest DM per user listing stalled TASK
// rows that just crossed a re-confirmation threshold (D+13, D+29).
func (r *ReminderService) DispatchStalledReconfirm(ctx context.Context) error {
	if r == nil || r.Slack == nil {
		return nil
	}
	users, err := store.GetAllUsers(ctx)
	if err != nil {
		return fmt.Errorf("get all users for stalled reconfirm: %w", err)
	}
	for _, u := range users {
		if strings.TrimSpace(u.SlackID) == "" {
			continue
		}
		if err := r.dispatchStalledReconfirmForUser(ctx, u); err != nil {
			logger.Warnf("[REMINDER] stalled reconfirm failed user=%s: %v", u.Email, err)
		}
	}
	return nil
}

func (r *ReminderService) dispatchStalledReconfirmForUser(ctx context.Context, u store.User) error {
	buckets, err := store.SelectStalled(ctx, u.Email, stalledReconfirmDays[0])
	if err != nil {
		return fmt.Errorf("select stalled: %w", err)
	}
	items := make([]store.StalledRequest, 0, len(buckets.Mine)+len(buckets.Observed))
	items = append(items, buckets.Mine...)
	items = append(items, buckets.Observed...)

	entries := collectStalledReconfirm(items)
	if len(entries) == 0 {
		return nil
	}
	if len(entries) > stalledReconfirmDigestCap {
		logger.Warnf("[REMINDER] stalled reconfirm digest capped user=%s skipped=%d",
			u.Email, len(entries)-stalledReconfirmDigestCap)
		entries = entries[:stalledReconfirmDigestCap]
	}

	if err := r.Slack.SendDM(ctx, u.SlackID, formatStalledDigest(entries)); err != nil {
		return fmt.Errorf("send dm: %w", err) // don't mark — retry next tick
	}

	now := time.Now().UTC()
	for _, e := range entries {
		if err := store.MarkReminded(ctx, u.Email, e.item.ID, e.item.Metadata, e.key, now); err != nil {
			logger.Warnf("[REMINDER] stalled reconfirm MarkReminded failed msg=%d: %v", e.item.ID, err)
		}
	}
	return nil
}

// collectStalledReconfirm picks, per item, the highest crossed threshold not yet
// reminded. Items with no crossed threshold, or whose highest crossed threshold was
// already reminded, are excluded (no fallback to a lower, unreminded threshold).
func collectStalledReconfirm(items []store.StalledRequest) []stalledReconfirmEntry {
	entries := make([]stalledReconfirmEntry, 0, len(items))
	for _, item := range items {
		key, ok := highestUnremindedStalledKey(item)
		if !ok {
			continue
		}
		entries = append(entries, stalledReconfirmEntry{item: item, key: key})
	}
	return entries
}

func highestUnremindedStalledKey(item store.StalledRequest) (string, bool) {
	threshold, ok := highestCrossedThreshold(item.DaysStalled, stalledReconfirmDays)
	if !ok {
		return "", false
	}
	key := fmt.Sprintf("stalled_reconfirm_d%d", threshold)
	if store.HasReminded(item.Metadata, key) {
		return "", false
	}
	return key, true
}

// highestCrossedThreshold returns the largest threshold <= days, if any.
func highestCrossedThreshold(days int, thresholds []int) (int, bool) {
	best, found := 0, false
	for _, t := range thresholds {
		if days >= t {
			best, found = t, true
		}
	}
	return best, found
}

func formatStalledDigest(entries []stalledReconfirmEntry) string {
	var b strings.Builder
	b.WriteString(":bell: 아직 진행 중인가요? 오래 멈춰있는 태스크입니다.\n")
	for _, e := range entries {
		b.WriteString(formatStalledDigestLine(e.item))
	}
	return b.String()
}

func formatStalledDigestLine(item store.StalledRequest) string {
	line := fmt.Sprintf("• %s (%s/%s, D+%d일)", item.Task, item.Source, item.Room, item.DaysStalled)
	if item.Link != "" {
		line += " " + item.Link
	}
	return line + "\n"
}
