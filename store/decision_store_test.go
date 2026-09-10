package store

import (
	"context"
	"testing"

	"message-consolidator/internal/testutil"
)

// TestRecordExtractionDecision covers the drop-site recorder, including the property that
// makes it safe to call from the extraction path: it must never fail loudly.
func TestRecordExtractionDecision(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenant := testutil.RandomEmail("tenant")

	RecordExtractionDecision(ctx, ExtractionDecisionInput{
		UserEmail: tenant,
		Stage:     DecisionStageFilter,
		Verdict:   DecisionVerdictNoise,
		Source:    "gmail",
		Text:      "[Daily Global Brief] 06/25 -- 8개국 비즈니스 데일리",
	})
	RecordExtractionDecision(ctx, ExtractionDecisionInput{
		UserEmail: tenant,
		Stage:     DecisionStageExtract,
		Verdict:   DecisionVerdictNone,
		Source:    "slack",
		Room:      "biz-global-thailand",
		Category:  "TASK",
		Text:      "FYI the office is closed on Monday",
	})

	var n int
	if err := GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM extraction_decisions WHERE user_email = ?`, tenant).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("recorded %d decisions, want 2", n)
	}

	var stage, verdict, source, head string
	if err := GetDB().QueryRowContext(ctx,
		`SELECT stage, verdict, source, text_head FROM extraction_decisions
		 WHERE user_email = ? AND stage = ?`, tenant, DecisionStageFilter).
		Scan(&stage, &verdict, &source, &head); err != nil {
		t.Fatalf("read filter row: %v", err)
	}
	if verdict != DecisionVerdictNoise || source != "gmail" {
		t.Errorf("filter row = verdict %q source %q", verdict, source)
	}
	if head == "" {
		t.Error("text_head empty; the excerpt is the point of the row")
	}

	// Why: a telemetry write must not be able to break extraction.
	t.Run("incomplete input is dropped, not an error", func(t *testing.T) {
		RecordExtractionDecision(ctx, ExtractionDecisionInput{UserEmail: tenant})
		var after int
		if err := GetDB().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM extraction_decisions WHERE user_email = ?`, tenant).Scan(&after); err != nil {
			t.Fatalf("recount: %v", err)
		}
		if after != 2 {
			t.Errorf("count changed to %d; incomplete input must be ignored", after)
		}
	})
}

func TestTruncateRunes_CountsRunesNotBytes(t *testing.T) {
	// Why: excerpts are often Korean, where a byte cap would slice a character in half.
	in := "가나다라마바사"
	if got := truncateRunes(in, 3); got != "가나다..." {
		t.Errorf("truncateRunes = %q, want %q", got, "가나다...")
	}
	if got := truncateRunes(in, 99); got != in {
		t.Errorf("short input mutated: %q", got)
	}
}
