package services

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

const (
	borderlineDigestMax    = 3
	borderlineBand         = 0.1
	borderlineWeekendHours = 72
	borderlineDayHours     = 24
	borderlineSnippetRunes = 90
	candidateDigestMax     = 5
)

var digestSlackEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func truncateDigestSnippet(text string, limit int) string {
	flat := strings.Join(strings.Fields(text), " ")
	r := []rune(flat)
	if len(r) <= limit {
		return flat
	}
	return string(r[:limit]) + "…"
}

// Why: the digest is sent Mon-Fri only, so the window is "since the previous send" and rows never repeat.
func borderlineSince(now time.Time) time.Time {
	if now.Weekday() == time.Monday {
		return now.Add(-borderlineWeekendHours * time.Hour)
	}
	return now.Add(-borderlineDayHours * time.Hour)
}

func (s *DailyDigestService) buildBorderlineBlocks(ctx context.Context, email string) []slack.Block {
	// Why: the band must sit under the threshold the filter actually applies to this user.
	hi := effectiveThreshold(ctx, email, s.Config.ClefThreshold)
	lo := hi - borderlineBand
	if lo < 0 {
		lo = 0
	}
	loc, err := time.LoadLocation(s.Config.Timezone)
	if err != nil {
		logger.Warnf("[DIGEST] timezone %q load failed, using UTC: %v", s.Config.Timezone, err)
		loc = time.UTC
	}
	since := borderlineSince(s.nowFn().In(loc))
	rows, err := store.ListBorderlineDrops(ctx, email, since, lo, hi, borderlineDigestMax)
	if err != nil {
		logger.Warnf("[DIGEST] borderline drops for %s: %v", email, err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	blocks := []slack.Block{
		slack.NewDividerBlock(),
		mrkdwnSection("*🔎 필터링된 메시지 확인 (경계선)*\n혹시 할 일이었던 메시지가 있나요? 학습 탭에서 확인해 주세요."),
	}
	for _, r := range rows {
		line := fmt.Sprintf("• %s  _%s/%s p=%.2f_", digestSlackEscaper.Replace(truncateDigestSnippet(r.Text, borderlineSnippetRunes)),
			digestSlackEscaper.Replace(r.Source), digestSlackEscaper.Replace(r.Room), r.PActionable)
		blocks = append(blocks, mrkdwnSection(line))
	}
	link := strings.TrimRight(s.Config.AppBaseURL, "/") + "/#learningTab"
	return append(blocks, mrkdwnSection(fmt.Sprintf("<%s|학습 탭에서 검토하기>", link)))
}

var candidateMarkupNeutralizer = strings.NewReplacer("*", " ", "_", " ", "~", " ", "`", " ")

// candidateText flattens whitespace, truncates, escapes Slack control chars and neutralizes mrkdwn emphasis markers.
func candidateText(text string) string {
	return digestSlackEscaper.Replace(candidateMarkupNeutralizer.Replace(truncateDigestSnippet(text, borderlineSnippetRunes)))
}

func (s *DailyDigestService) buildCandidateBlocks(ctx context.Context, email string) []slack.Block {
	rows, err := store.ListPendingCompletionCandidates(ctx, email, candidateDigestMax)
	if err != nil {
		logger.Warnf("[DIGEST] pending completion candidates for %s: %v", email, err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	blocks := []slack.Block{
		slack.NewDividerBlock(),
		mrkdwnSection("*✅ 완료로 보이는 태스크 (확인 필요)*"),
	}
	for _, r := range rows {
		line := fmt.Sprintf("• %s  (%s/%s) — \"%s\"", candidateText(r.Task), candidateText(r.Source), candidateText(r.Room), candidateText(r.SourceText))
		blocks = append(blocks, mrkdwnSection(line))
	}
	link := strings.TrimRight(s.Config.AppBaseURL, "/")
	return append(blocks, mrkdwnSection(fmt.Sprintf("<%s|앱에서 확인하기>", link)))
}

func mrkdwnSection(text string) slack.Block {
	return slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, text, false, false), nil, nil)
}
