package services

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
)

func TestLinkFilterOutcome_TaskVsNone(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()
	ctx := context.Background()
	email := testutil.RandomEmail("outcome")

	insert := func(verdict, ts string) int64 {
		id, err := store.InsertFilterScore(ctx, store.FilterScoreInput{
			UserEmail: email, Source: "slack", Room: "r", SourceTS: ts, Provider: "clef",
			PActionable: 0.6, Threshold: 0.3, Verdict: verdict, Text: "t",
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	passTask := insert(store.FilterVerdictPass, "ts-task")
	passNone := insert(store.FilterVerdictPass, "ts-none")
	noise := insert(store.FilterVerdictNoise, "ts-noise")

	LinkFilterOutcome(ctx, email, "slack", "r", "ts-task", true)
	LinkFilterOutcome(ctx, email, "slack", "r", "ts-none", false)
	LinkFilterOutcome(ctx, email, "slack", "r", "ts-noise", true)

	want := map[int64]string{passTask: store.FilterOutcomeTask, passNone: store.FilterOutcomeNone, noise: ""}
	for id, outcome := range want {
		row, err := store.GetFilterScore(ctx, email, id)
		if err != nil {
			t.Fatal(err)
		}
		if row.Outcome != outcome {
			t.Errorf("row %d outcome = %q, want %q", id, row.Outcome, outcome)
		}
	}
}
