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

func seedPassRow(t *testing.T, in store.FilterScoreInput) int64 {
	t.Helper()
	in.Provider, in.PActionable, in.Threshold, in.Verdict = "clef", 0.6, 0.3, store.FilterVerdictPass
	id, err := store.InsertFilterScore(context.Background(), in)
	if err != nil {
		t.Fatalf("seed pass row: %v", err)
	}
	return id
}

func TestRecordSingleDeletion_RelabelsRowAlreadyLinkedAsTask(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	ctx := context.Background()
	email := testutil.RandomEmail("fb-task")
	id := seedFilterRow(t, email, store.FilterVerdictPass, "general", "ts-9", "x")
	LinkFilterOutcome(ctx, email, "slack", "general", "ts-9", true)

	recordSingleDeletion(ctx, email, store.ConsolidatedMessage{ID: 9, Source: "slack", Room: "general", SourceTS: "ts-9"})

	if got := filterOutcome(t, email, id); got != store.FilterOutcomeDeletedNoise {
		t.Errorf("outcome = %q, want deleted_noise over the earlier task link", got)
	}
}

func TestRecordSingleDeletion_MatchesNonLatestMessageOfGroup(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("fb-group")
	id := seedPassRow(t, store.FilterScoreInput{
		UserEmail: email, Source: "whatsapp", Room: "team", SourceTS: "m3", MessageRef: "m1,m2,m3",
	})
	prefixID := seedPassRow(t, store.FilterScoreInput{
		UserEmail: email, Source: "whatsapp", Room: "team", SourceTS: "m22", MessageRef: "m21,m22",
	})

	recordSingleDeletion(context.Background(), email, store.ConsolidatedMessage{ID: 3, Source: "whatsapp", Room: "team", SourceTS: "m2"})

	if got := filterOutcome(t, email, id); got != store.FilterOutcomeDeletedNoise {
		t.Errorf("group row outcome = %q, want deleted_noise", got)
	}
	if got := filterOutcome(t, email, prefixID); got != "" {
		t.Errorf("partial id match must not label, got %q", got)
	}
}

func TestRecordSingleDeletion_GmailTaskRoomMatchesFilterRow(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("fb-gmail")
	id := seedPassRow(t, store.FilterScoreInput{
		UserEmail: email, Source: store.SourceGmail, SourceTS: "gmail-abc", MessageRef: "abc",
	})

	recordSingleDeletion(context.Background(), email, store.ConsolidatedMessage{ID: 4, Source: store.SourceGmail, Room: "Gmail", SourceTS: "gmail-abc"})

	if got := filterOutcome(t, email, id); got != store.FilterOutcomeDeletedNoise {
		t.Errorf("gmail row outcome = %q, want deleted_noise", got)
	}
}

func TestRecordSingleDeletion_NeverLabelsAnotherUsersRow(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	owner := testutil.RandomEmail("fb-owner")
	id := seedFilterRow(t, owner, store.FilterVerdictPass, "general", "ts-x", "x")

	recordSingleDeletion(context.Background(), testutil.RandomEmail("fb-other"),
		store.ConsolidatedMessage{ID: 5, Source: "slack", Room: "general", SourceTS: "ts-x"})

	if got := filterOutcome(t, owner, id); got != "" {
		t.Errorf("cross-user deletion labeled row: %q", got)
	}
}
