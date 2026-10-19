package services

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"

	"github.com/slack-go/slack"
)

const (
	borderlineDigestMax    = 3
	borderlineBand         = 0.1
	borderlineLookbackDays = 7
	borderlineSnippetRunes = 90
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

func (s *DailyDigestService) buildBorderlineBlocks(ctx context.Context, email string) []slack.Block {
	// Why: the band must sit under the threshold the filter actually applies to this user.
	hi := effectiveThreshold(ctx, email, s.Config.ClefThreshold)
	lo := hi - borderlineBand
	if lo < 0 {
		lo = 0
	}
	since := s.nowFn().AddDate(0, 0, -borderlineLookbackDays)
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

func mrkdwnSection(text string) slack.Block {
	return slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, text, false, false), nil, nil)
}
