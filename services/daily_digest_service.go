package services

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"
	"unicode/utf8"
)

const maxTaskRunes = 80

// DailyDigestService dispatches the daily task summary via Slack DM.
type DailyDigestService struct {
	Slack          SlackPoster
	RecipientEmail string
	Limit          int
	Timezone       string
}

// NewDailyDigestService constructs a DailyDigestService.
func NewDailyDigestService(slack SlackPoster, recipientEmail string, limit int, tz string) *DailyDigestService {
	return &DailyDigestService{
		Slack:          slack,
		RecipientEmail: recipientEmail,
		Limit:          limit,
		Timezone:       tz,
	}
}

// Dispatch builds and sends the daily digest DM to the configured recipient.
func (s *DailyDigestService) Dispatch(ctx context.Context) error {
	if s == nil || s.Slack == nil || s.RecipientEmail == "" {
		return nil
	}

	user, err := store.GetOrCreateUser(ctx, s.RecipientEmail, "", "")
	if err != nil || user == nil || strings.TrimSpace(user.SlackID) == "" {
		logger.Warnf("[DIGEST] no slack id for %s", s.RecipientEmail)
		return nil
	}

	snap, err := store.GetDailyDigest(ctx, s.RecipientEmail, s.Limit)
	if err != nil {
		return fmt.Errorf("digest: get snapshot: %w", err)
	}

	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		loc = time.UTC
	}
	text := formatDigest(snap, time.Now().In(loc))

	return s.Slack.SendDM(ctx, user.SlackID, text)
}

// formatDigest builds the Slack message text for the daily digest.
func formatDigest(snap store.DigestSnapshot, now time.Time) string {
	var sb strings.Builder

	dateStr := now.Format("2006-01-02")
	weekday := koreanWeekday(now.Weekday())
	fmt.Fprintf(&sb, ":bookmark_tabs: *%s (%s) 일일 업무 요약*\n\n", dateStr, weekday)

	// Received tasks
	fmt.Fprintf(&sb, "📥 *받은 업무* — %d건\n", snap.ReceivedTotal)
	if len(snap.Received) == 0 {
		sb.WriteString("   (없음)\n")
	} else {
		for _, t := range snap.Received {
			sb.WriteString(formatTaskLine(t, true))
		}
		remainder := snap.ReceivedTotal - len(snap.Received)
		if remainder > 0 {
			fmt.Fprintf(&sb, "   …외 %d건\n", remainder)
		}
	}

	sb.WriteString("\n")

	// Assigned tasks
	fmt.Fprintf(&sb, "📤 *맡긴 업무* — %d건\n", snap.AssignedTotal)
	if len(snap.Assigned) == 0 {
		sb.WriteString("   (없음)\n")
	} else {
		for _, t := range snap.Assigned {
			sb.WriteString(formatTaskLine(t, false))
		}
		remainder := snap.AssignedTotal - len(snap.Assigned)
		if remainder > 0 {
			fmt.Fprintf(&sb, "   …외 %d건\n", remainder)
		}
	}

	return sb.String()
}

// formatTaskLine formats a single DigestTask line with optional room, counterpart, and deadline.
// incoming=true → 받은 업무(상대가 요청자, "←" 화살표); incoming=false → 맡긴 업무(상대가 수임자, "→" 화살표).
func formatTaskLine(t store.DigestTask, incoming bool) string {
	task := truncateRunes(t.Task, maxTaskRunes)

	prefix := buildSourcePrefix(t.Source, t.Room)

	suffix := buildSuffix(t.Counterpart, t.Deadline.String, t.Deadline.Valid, incoming)

	if suffix != "" {
		return fmt.Sprintf("• %s%s (%s)\n", prefix, task, suffix)
	}
	return fmt.Sprintf("• %s%s\n", prefix, task)
}

func buildSourcePrefix(source, room string) string {
	if source == "" && room == "" {
		return ""
	}
	if room == "" {
		return fmt.Sprintf("[%s] ", source)
	}
	if source == "" {
		return fmt.Sprintf("[%s] ", room)
	}
	return fmt.Sprintf("[%s/%s] ", source, room)
}

func buildSuffix(counterpart, deadline string, deadlineValid, incoming bool) string {
	parts := make([]string, 0, 2)
	if counterpart != "" {
		arrow := "→"
		if incoming {
			arrow = "←"
		}
		parts = append(parts, arrow+" "+counterpart)
	}
	if deadlineValid && deadline != "" {
		// Format as ~MM-DD if the deadline contains date info.
		parts = append(parts, "~"+formatDeadlineShort(deadline))
	}
	return strings.Join(parts, ", ")
}

// formatDeadlineShort extracts MM-DD from a deadline string like "2026-04-30" or "2026-04-30T00:00:00Z".
func formatDeadlineShort(deadline string) string {
	if len(deadline) >= 10 {
		return deadline[5:10]
	}
	return deadline
}

// truncateRunes cuts s to at most maxRunes runes, appending "…" if truncated.
func truncateRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "…"
}

// koreanWeekday maps time.Weekday to Korean single-character weekday names.
func koreanWeekday(w time.Weekday) string {
	switch w {
	case time.Monday:
		return "월"
	case time.Tuesday:
		return "화"
	case time.Wednesday:
		return "수"
	case time.Thursday:
		return "목"
	case time.Friday:
		return "금"
	case time.Saturday:
		return "토"
	default:
		return "일"
	}
}
