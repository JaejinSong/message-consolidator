package services

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"testing"
	"time"

	"message-consolidator/db"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

var calibNow = time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)

func drop(p float64, outcome string) db.FilterScore {
	return db.FilterScore{Verdict: store.FilterVerdictNoise, PActionable: p, Outcome: outcome,
		CreatedAt: sql.NullTime{Time: calibNow.Add(-24 * time.Hour), Valid: true}}
}

func passes(n int, outcome string, ageDays int) []db.FilterScore {
	out := make([]db.FilterScore, n)
	for i := range out {
		out[i] = db.FilterScore{Verdict: store.FilterVerdictPass, PActionable: 0.8, Outcome: outcome,
			CreatedAt: sql.NullTime{Time: calibNow.Add(-time.Duration(ageDays) * 24 * time.Hour), Valid: true}}
	}
	return out
}

func concat(parts ...[]db.FilterScore) []db.FilterScore {
	var out []db.FilterScore
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestCalibrationProposal(t *testing.T) {
	tests := []struct {
		name     string
		rows     []db.FilterScore
		current  float64
		wantOK   bool
		wantNext float64
	}{
		{"no rows", nil, 0.3, false, 0},
		{"false drop moves down by 0.05 under min p", []db.FilterScore{drop(0.27, store.FilterOutcomeRescued), drop(0.22, store.FilterOutcomeManualMatch)}, 0.3, true, 0.17},
		{"single false drop is enough evidence", []db.FilterScore{drop(0.25, store.FilterOutcomeRescued)}, 0.3, true, 0.2},
		{"down is clamped at 0.1", []db.FilterScore{drop(0.12, store.FilterOutcomeRescued)}, 0.3, true, 0.1},
		{"already at floor yields nothing", []db.FilterScore{drop(0.12, store.FilterOutcomeRescued)}, 0.1, false, 0},
		{"false drop above current threshold does not raise", []db.FilterScore{drop(0.45, store.FilterOutcomeRescued)}, 0.3, false, 0},
		{"confirmed noise is not a false drop", []db.FilterScore{drop(0.2, store.FilterOutcomeConfirmedNoise)}, 0.3, false, 0},
		{"false drop blocks upward move", concat([]db.FilterScore{drop(0.25, store.FilterOutcomeRescued)}, passes(30, store.FilterOutcomeNone, 30)), 0.3, true, 0.2},
		{"high false-pass rate moves up by 0.05", concat(passes(15, store.FilterOutcomeNone, 30), passes(14, store.FilterOutcomeTask, 2)), 0.3, true, 0.35},
		{"up counts deleted_noise as false pass", concat(passes(12, store.FilterOutcomeDeletedNoise, 30), passes(17, store.FilterOutcomeTask, 2)), 0.3, true, 0.35},
		{"up needs 29 labeled passes", concat(passes(28, store.FilterOutcomeNone, 30)), 0.3, false, 0},
		{"up needs rate >= 0.4", concat(passes(11, store.FilterOutcomeNone, 30), passes(18, store.FilterOutcomeTask, 2)), 0.3, false, 0},
		{"unlabeled passes are not counted", concat(passes(40, "", 30), passes(10, store.FilterOutcomeNone, 30)), 0.3, false, 0},
		{"up needs about a month of history", concat(passes(29, store.FilterOutcomeNone, 5)), 0.3, false, 0},
		{"up is clamped at 0.5", concat(passes(29, store.FilterOutcomeNone, 30)), 0.48, true, 0.5},
		{"already at ceiling yields nothing", concat(passes(29, store.FilterOutcomeNone, 30)), 0.5, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := calibrationProposal(tt.rows, tt.current, calibNow)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (proposal %v)", ok, tt.wantOK, got)
			}
			if ok && math.Abs(got-tt.wantNext) > 1e-9 {
				t.Errorf("proposal = %v, want %v", got, tt.wantNext)
			}
		})
	}
}

func TestCalibrateUserThreshold_ProposesOnceAndApprovalWritesSetting(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	ctx := context.Background()
	email := testutil.RandomEmail("calib")
	id := seedFilterRow(t, email, store.FilterVerdictNoise, "r", "ts", "text")
	if err := store.SetFilterOutcome(ctx, email, id, store.FilterOutcomeRescued); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := CalibrateUserThreshold(ctx, email, 0.3); err != nil {
			t.Fatalf("calibrate: %v", err)
		}
	}
	obs, ok := getObservation(t, email, ObservationKindFilterThreshold, "0.30", "0.15", "clef")
	if !ok || obs.Status != "pending" {
		t.Fatalf("expected one pending proposal 0.30->0.15, got %+v ok=%v", obs, ok)
	}
	var n int
	if err := store.GetDB().QueryRow(`SELECT COUNT(*) FROM correction_observations WHERE user_email = ?`, email).Scan(&n); err != nil || n != 1 {
		t.Fatalf("observation count = %d err=%v, want 1", n, err)
	}
	if _, ok := store.GetUserClefThreshold(ctx, email); ok {
		t.Fatal("calibrator must not apply the threshold itself")
	}

	if err := DecideObservation(ctx, email, obs.ID, true); err != nil {
		t.Fatalf("approve: %v", err)
	}
	got, ok := store.GetUserClefThreshold(ctx, email)
	if !ok || fmt.Sprintf("%.2f", got) != "0.15" {
		t.Fatalf("threshold after approval = %v ok=%v, want 0.15", got, ok)
	}
}

func TestDecideObservation_RejectedThresholdWritesNothingAndStaysRejected(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	ctx := context.Background()
	email := testutil.RandomEmail("calib-rej")
	id := seedFilterRow(t, email, store.FilterVerdictNoise, "r", "ts", "text")
	if err := store.SetFilterOutcome(ctx, email, id, store.FilterOutcomeManualMatch); err != nil {
		t.Fatal(err)
	}
	if err := CalibrateUserThreshold(ctx, email, 0.3); err != nil {
		t.Fatal(err)
	}
	obs, _ := getObservation(t, email, ObservationKindFilterThreshold, "0.30", "0.15", "clef")
	if err := DecideObservation(ctx, email, obs.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.GetUserClefThreshold(ctx, email); ok {
		t.Error("rejection must not write the setting")
	}
	if err := CalibrateUserThreshold(ctx, email, 0.3); err != nil {
		t.Fatal(err)
	}
	if after, _ := getObservation(t, email, ObservationKindFilterThreshold, "0.30", "0.15", "clef"); after.Status != "rejected" {
		t.Errorf("rejected proposal resurrected: %+v", after)
	}
}

func TestDecideObservation_ApproveRejectsOutOfRangeThreshold(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	ctx := context.Background()
	email := testutil.RandomEmail("calib-bad")
	if _, err := upsertCorrectionObservation(ctx, email, ObservationKindFilterThreshold, "0.30", "0.90", "clef", 1, 1); err != nil {
		t.Fatal(err)
	}
	obs, _ := getObservation(t, email, ObservationKindFilterThreshold, "0.30", "0.90", "clef")
	if err := DecideObservation(ctx, email, obs.ID, true); err == nil {
		t.Fatal("expected error for out-of-range threshold")
	}
	if _, ok := store.GetUserClefThreshold(ctx, email); ok {
		t.Error("out-of-range value must not be written")
	}
}

func TestFilterThresholdExcludedFromSuppressRules(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	ctx := context.Background()
	email := testutil.RandomEmail("calib-g6")
	if _, err := upsertCorrectionObservation(ctx, email, ObservationKindFilterThreshold, "0.30", "0.15", "clef", 1, 1); err != nil {
		t.Fatal(err)
	}
	obs, _ := getObservation(t, email, ObservationKindFilterThreshold, "0.30", "0.15", "clef")
	if err := DecideObservation(ctx, email, obs.ID, true); err != nil {
		t.Fatal(err)
	}
	rules, err := db.New(store.GetDB()).ListActiveSuppressRules(ctx, email)
	if err != nil || len(rules) != 0 {
		t.Fatalf("filter_threshold leaked into suppress rules: %v err=%v", rules, err)
	}
}

func TestCalibrateAll_PrunesOldScores(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	ctx := context.Background()
	email := testutil.RandomEmail("calib-prune")
	oldID := seedFilterRow(t, email, store.FilterVerdictPass, "r", "old", "x")
	newID := seedFilterRow(t, email, store.FilterVerdictPass, "r", "new", "x")
	if _, err := store.GetDB().Exec(`UPDATE filter_scores SET created_at = datetime('now','-40 days') WHERE id = ?`, oldID); err != nil {
		t.Fatal(err)
	}
	if err := CalibrateAllFilterThresholds(ctx, 0.3); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetFilterScore(ctx, email, oldID); err == nil {
		t.Error("40-day-old row must be pruned")
	}
	if _, err := store.GetFilterScore(ctx, email, newID); err != nil {
		t.Errorf("fresh row must survive: %v", err)
	}
}
