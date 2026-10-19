package services

import (
	"context"
	"testing"

	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func seedFilterRow(t *testing.T, email, verdict, room, ts, text string) int64 {
	t.Helper()
	id, err := store.InsertFilterScore(context.Background(), store.FilterScoreInput{
		UserEmail: email, Source: "slack", Room: room, SourceTS: ts, Provider: "clef",
		PActionable: 0.2, Threshold: 0.3, Verdict: verdict, Text: text,
	})
	if err != nil {
		t.Fatalf("seed filter row: %v", err)
	}
	return id
}

func filterOutcome(t *testing.T, email string, id int64) string {
	t.Helper()
	row, err := store.GetFilterScore(context.Background(), email, id)
	if err != nil {
		t.Fatalf("get filter score: %v", err)
	}
	return row.Outcome
}

func TestRecordSingleDeletion_LabelsPassedFilterRow(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("fb-del")
	passID := seedFilterRow(t, email, store.FilterVerdictPass, "general", "ts-1", "x")
	noiseID := seedFilterRow(t, email, store.FilterVerdictNoise, "general", "ts-1", "x")

	m := store.ConsolidatedMessage{ID: 1, Source: "slack", Room: "general", SourceTS: "ts-1"}
	recordSingleDeletion(context.Background(), email, m)

	if got := filterOutcome(t, email, passID); got != store.FilterOutcomeDeletedNoise {
		t.Errorf("pass row outcome = %q, want deleted_noise", got)
	}
	if got := filterOutcome(t, email, noiseID); got != "" {
		t.Errorf("noise row must stay unlabeled, got %q", got)
	}
}

func TestRecordSingleDeletion_DoneTaskDoesNotLabelFilterRow(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("fb-done")
	passID := seedFilterRow(t, email, store.FilterVerdictPass, "general", "ts-2", "x")

	m := store.ConsolidatedMessage{ID: 2, Source: "slack", Room: "general", SourceTS: "ts-2", Done: true}
	recordSingleDeletion(context.Background(), email, m)

	if got := filterOutcome(t, email, passID); got != "" {
		t.Errorf("done deletion must not label, got %q", got)
	}
}

func TestMatchManualAddToFilterDrop(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("fb-manual")
	match := seedFilterRow(t, email, store.FilterVerdictNoise, "r", "a", "please send quarterly budget report tomorrow")
	other := seedFilterRow(t, email, store.FilterVerdictNoise, "r", "b", "lunch menu today pizza")
	pass := seedFilterRow(t, email, store.FilterVerdictPass, "r", "c", "send quarterly budget report")

	matchManualAddToFilterDrop(context.Background(), email, "Send quarterly budget report", "")

	if got := filterOutcome(t, email, match); got != store.FilterOutcomeManualMatch {
		t.Errorf("matching drop outcome = %q, want manual_match", got)
	}
	if got := filterOutcome(t, email, other); got != "" {
		t.Errorf("unrelated drop labeled: %q", got)
	}
	if got := filterOutcome(t, email, pass); got != "" {
		t.Errorf("pass row must not be labeled: %q", got)
	}
}

func TestMatchManualAddToFilterDrop_TooFewSharedTokens(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("fb-few")
	id := seedFilterRow(t, email, store.FilterVerdictNoise, "r", "a", "send budget report")

	matchManualAddToFilterDrop(context.Background(), email, "send budget notes", "")

	if got := filterOutcome(t, email, id); got != "" {
		t.Errorf("2 shared tokens must not match, got %q", got)
	}
}
