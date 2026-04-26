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
