package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"message-consolidator/internal/testutil"
)

func setupFilterStoreTest(t *testing.T) {
	t.Helper()
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(cleanup)
}

func TestMigration_CreatesFilterScoresTable(t *testing.T) {
	setupFilterStoreTest(t)

	var tables, indexes int
	q := `SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name LIKE ?`
	if err := GetDB().QueryRow(q, "table", "filter_scores").Scan(&tables); err != nil || tables != 1 {
		t.Fatalf("filter_scores table count = %d, err = %v", tables, err)
	}
	if err := GetDB().QueryRow(q, "index", "idx_filter_scores_%").Scan(&indexes); err != nil || indexes != 3 {
		t.Fatalf("filter_scores index count = %d, err = %v; want 3", indexes, err)
	}
}

func newScore(email, verdict string, p float64) FilterScoreInput {
	return FilterScoreInput{
		UserEmail: email, Source: "slack", Room: "r", SourceTS: "ts1", Provider: "clef",
		PActionable: p, Threshold: 0.3, Verdict: verdict, Text: "t",
	}
}

func TestFilterScoreStore_RoundTripAndScoping(t *testing.T) {
	setupFilterStoreTest(t)
	ctx := context.Background()
	a, b := testutil.RandomEmail("a"), testutil.RandomEmail("b")
	since := time.Now().Add(-time.Hour)

	idA, err := InsertFilterScore(ctx, newScore(a, FilterVerdictNoise, 0.25))
	if err != nil || idA == 0 {
		t.Fatalf("insert: id=%d err=%v", idA, err)
	}
	if _, err := InsertFilterScore(ctx, newScore(a, FilterVerdictPass, 0.9)); err != nil {
		t.Fatal(err)
	}
	if _, err := InsertFilterScore(ctx, newScore(b, FilterVerdictNoise, 0.1)); err != nil {
		t.Fatal(err)
	}

	if _, err := GetFilterScore(ctx, b, idA); err == nil {
		t.Error("GetFilterScore must be user-scoped")
	}
	drops, err := ListFilterDrops(ctx, a, since, 10)
	if err != nil || len(drops) != 1 || drops[0].ID != idA {
		t.Fatalf("drops = %+v, err = %v", drops, err)
	}
	if future, _ := ListFilterDrops(ctx, a, time.Now().Add(time.Hour), 10); len(future) != 0 {
		t.Errorf("since in the future must exclude rows, got %d", len(future))
	}
	users, err := ListFilterUsers(ctx, since)
	if err != nil || !contains(users, a) || !contains(users, b) {
		t.Fatalf("users = %v, err = %v", users, err)
	}
	all, err := ListFilterScoresSince(ctx, a, since)
	if err != nil || len(all) != 2 {
		t.Fatalf("scores = %d, err = %v; want 2", len(all), err)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func TestFilterScoreStore_BorderlineAndOutcomes(t *testing.T) {
	setupFilterStoreTest(t)
	ctx := context.Background()
	email := testutil.RandomEmail("border")
	since := time.Now().Add(-time.Hour)

	low, _ := InsertFilterScore(ctx, newScore(email, FilterVerdictNoise, 0.05))
	mid, _ := InsertFilterScore(ctx, newScore(email, FilterVerdictNoise, 0.22))
	high, _ := InsertFilterScore(ctx, newScore(email, FilterVerdictNoise, 0.28))
	_ = low

	got, err := ListBorderlineDrops(ctx, email, since, 0.2, 0.3, 10)
	if err != nil || len(got) != 2 || got[0].ID != high || got[1].ID != mid {
		t.Fatalf("borderline = %+v, err = %v; want [high, mid]", got, err)
	}

	if err := SetFilterOutcome(ctx, email, high, FilterOutcomeRescued); err != nil {
		t.Fatal(err)
	}
	row, _ := GetFilterScore(ctx, email, high)
	if row.Outcome != FilterOutcomeRescued || !row.OutcomeAt.Valid {
		t.Errorf("outcome not set: %+v", row)
	}
	if got, _ := ListBorderlineDrops(ctx, email, since, 0.2, 0.3, 10); len(got) != 1 {
		t.Errorf("labeled drop must leave the borderline list, got %d", len(got))
	}
}

func TestFilterScoreStore_LinkOutcomeOnlyUnlabeledPass(t *testing.T) {
	setupFilterStoreTest(t)
	ctx := context.Background()
	email := testutil.RandomEmail("link")

	pass, _ := InsertFilterScore(ctx, newScore(email, FilterVerdictPass, 0.9))
	noise, _ := InsertFilterScore(ctx, newScore(email, FilterVerdictNoise, 0.1))

	if err := LinkFilterOutcomeBySourceTS(ctx, email, "slack", "r", "ts1", FilterOutcomeTask); err != nil {
		t.Fatal(err)
	}
	if err := LinkFilterOutcomeBySourceTS(ctx, email, "slack", "r", "ts1", FilterOutcomeNone); err != nil {
		t.Fatal(err)
	}
	p, _ := GetFilterScore(ctx, email, pass)
	n, _ := GetFilterScore(ctx, email, noise)
	if p.Outcome != FilterOutcomeTask {
		t.Errorf("pass outcome = %q, want task (first link wins)", p.Outcome)
	}
	if n.Outcome != "" {
		t.Errorf("noise row must not be linked, got %q", n.Outcome)
	}
}

func TestFilterScoreStore_DeleteBeforeAndTextCap(t *testing.T) {
	setupFilterStoreTest(t)
	ctx := context.Background()
	email := testutil.RandomEmail("del")

	in := newScore(email, FilterVerdictPass, 0.9)
	in.Text = strings.Repeat("가", FilterScoreTextMaxRunes+10)
	id, err := InsertFilterScore(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	row, _ := GetFilterScore(ctx, email, id)
	if n := len([]rune(row.Text)); n != FilterScoreTextMaxRunes {
		t.Errorf("text runes = %d, want %d", n, FilterScoreTextMaxRunes)
	}

	if err := DeleteFilterScoresBefore(ctx, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := GetFilterScore(ctx, email, id); err != nil {
		t.Error("fresh row must survive a past cutoff")
	}
	if err := DeleteFilterScoresBefore(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := GetFilterScore(ctx, email, id); err == nil {
		t.Error("row must be deleted by a future cutoff")
	}
}
