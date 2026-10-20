package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func blocksText(blocks []slack.Block) string {
	var sb strings.Builder
	for _, b := range blocks {
		if s, ok := b.(*slack.SectionBlock); ok && s.Text != nil {
			sb.WriteString(s.Text.Text + "\n")
		}
	}
	return sb.String()
}

func seedBorderline(t *testing.T, email string, p float64, text, outcome string) {
	t.Helper()
	id, err := store.InsertFilterScore(context.Background(), store.FilterScoreInput{
		UserEmail: email, Source: store.SourceSlack, Room: "r", SourceTS: "1.1",
		Provider: "clef", PActionable: p, Threshold: 0.3, Verdict: store.FilterVerdictNoise, Text: text,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if outcome != "" {
		if err := store.SetFilterOutcome(context.Background(), email, id, outcome); err != nil {
			t.Fatalf("outcome: %v", err)
		}
	}
}

func TestBorderlineDigestBlocks_CapsAndFiltersAndLinks(t *testing.T) {
	defer setupReviewDB(t)()
	email := testutil.RandomEmail("digest")
	for i := 0; i < 5; i++ {
		seedBorderline(t, email, 0.25, "borderline message <b>&", "")
	}
	seedBorderline(t, email, 0.05, "too low", "")
	seedBorderline(t, email, 0.29, "already labeled", store.FilterOutcomeRescued)

	s := &DailyDigestService{nowFn: time.Now, Config: DailyDigestConfig{ClefThreshold: 0.3, AppBaseURL: "https://app.example"}}
	blocks := s.buildBorderlineBlocks(context.Background(), email)
	text := blocksText(blocks)

	if got := strings.Count(text, "borderline message"); got != borderlineDigestMax {
		t.Errorf("rows = %d, want %d", got, borderlineDigestMax)
	}
	if strings.Contains(text, "too low") || strings.Contains(text, "already labeled") {
		t.Errorf("out-of-range or labeled rows leaked: %s", text)
	}
	if strings.Contains(text, "<b>") || !strings.Contains(text, "&lt;b&gt;&amp;") {
		t.Errorf("text not escaped: %s", text)
	}
	if !strings.Contains(text, "<https://app.example/#learningTab|") {
		t.Errorf("missing learning link: %s", text)
	}
}

func TestBorderlineDigestBlocks_EmptyAndOtherUser(t *testing.T) {
	defer setupReviewDB(t)()
	email := testutil.RandomEmail("digest")
	seedBorderline(t, testutil.RandomEmail("other"), 0.25, "someone else", "")
	s := &DailyDigestService{nowFn: time.Now, Config: DailyDigestConfig{ClefThreshold: 0.3}}
	if blocks := s.buildBorderlineBlocks(context.Background(), email); blocks != nil {
		t.Errorf("want nil, got %d blocks", len(blocks))
	}
}

func TestTruncateDigestSnippet(t *testing.T) {
	got := truncateDigestSnippet("a\nb  "+strings.Repeat("가", 200), 10)
	if []rune(got)[len([]rune(got))-1] != '…' || strings.Contains(got, "\n") {
		t.Errorf("got %q", got)
	}
}

func TestBorderlineDigestBlocks_UsesPerUserThreshold(t *testing.T) {
	defer setupReviewDB(t)()
	ctx := context.Background()
	email := testutil.RandomEmail("digest-user")
	seedBorderline(t, email, 0.15, "lowered band row", "")
	seedBorderline(t, email, 0.25, "above user threshold", "")
	if err := store.UpsertSetting(ctx, store.ClefThresholdKey(email), "0.20", email); err != nil {
		t.Fatalf("set threshold: %v", err)
	}
	defer func() { _ = store.UpsertSetting(ctx, store.ClefThresholdKey(email), "", email) }()

	s := &DailyDigestService{nowFn: time.Now, Config: DailyDigestConfig{ClefThreshold: 0.3}}
	text := blocksText(s.buildBorderlineBlocks(ctx, email))

	if !strings.Contains(text, "lowered band row") {
		t.Errorf("row in [0.10, 0.20) missing: %s", text)
	}
	if strings.Contains(text, "above user threshold") {
		t.Errorf("row above the user's threshold leaked: %s", text)
	}
}

func TestBorderlineSince_MondayCoversWeekendOtherDaysOneDay(t *testing.T) {
	monday := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	tuesday := monday.AddDate(0, 0, 1)

	if got := monday.Sub(borderlineSince(monday)); got != 72*time.Hour {
		t.Errorf("monday lookback = %v, want 72h", got)
	}
	if got := tuesday.Sub(borderlineSince(tuesday)); got != 24*time.Hour {
		t.Errorf("tuesday lookback = %v, want 24h", got)
	}
}
