package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"message-consolidator/db"
	"message-consolidator/logger"
	"message-consolidator/store"
)

// The calibrator proposes a per-user Clef threshold from filter feedback. Why confirm-first:
// kind='filter_threshold' sits outside ListActiveSuppressRules' kind='suppress' filter, and
// only an approved DecideObservation writes the app_settings key, so nothing applies itself.
const (
	ObservationKindFilterThreshold = "filter_threshold"

	filterCalibrationScope   = "clef"
	filterRetentionDays      = 31
	filterDownStep           = 0.05
	filterUpStep             = 0.05
	filterUpMinLabeledPasses = 29
	filterUpMinFalsePassRate = 0.4
	// Why: raising the threshold needs a full quiet month, so require data spanning about the window.
	filterUpMinHistoryDays = 29
)

func isFalseDrop(r db.FilterScore) bool {
	if r.Verdict != store.FilterVerdictNoise {
		return false
	}
	return r.Outcome == store.FilterOutcomeRescued || r.Outcome == store.FilterOutcomeManualMatch
}

func roundThreshold(v float64) float64 {
	return math.Round(v*100) / 100
}

// Why: missing a real task costs far more than passing noise, so down moves on one false drop
// while up needs a quiet month and a high false-pass rate.
func calibrationProposal(rows []db.FilterScore, current float64, now time.Time) (float64, bool) {
	minFalseDrop, hasFalseDrop := 1.0, false
	labeled, falsePass := 0, 0
	oldest := now
	for _, r := range rows {
		if r.CreatedAt.Valid && r.CreatedAt.Time.Before(oldest) {
			oldest = r.CreatedAt.Time
		}
		if isFalseDrop(r) {
			hasFalseDrop = true
			minFalseDrop = math.Min(minFalseDrop, r.PActionable)
			continue
		}
		if r.Verdict != store.FilterVerdictPass {
			continue
		}
		switch r.Outcome {
		case store.FilterOutcomeTask:
			labeled++
		case store.FilterOutcomeNone, store.FilterOutcomeDeletedNoise:
			labeled++
			falsePass++
		}
	}
	if hasFalseDrop {
		next := roundThreshold(math.Max(store.FilterThresholdMin, minFalseDrop-filterDownStep))
		return next, next < current
	}
	if labeled < filterUpMinLabeledPasses || now.Sub(oldest) < filterUpMinHistoryDays*24*time.Hour {
		return 0, false
	}
	if float64(falsePass)/float64(labeled) < filterUpMinFalsePassRate {
		return 0, false
	}
	next := roundThreshold(math.Min(store.FilterThresholdMax, current+filterUpStep))
	return next, next > current
}

func effectiveThreshold(ctx context.Context, email string, fallback float64) float64 {
	if v, ok := store.GetUserClefThreshold(ctx, email); ok {
		return v
	}
	return fallback
}

func CalibrateAllFilterThresholds(ctx context.Context, defaultThreshold float64) error {
	since := time.Now().AddDate(0, 0, -filterRetentionDays)
	users, err := store.ListFilterUsers(ctx, since)
	if err != nil {
		return fmt.Errorf("calibrate: %w", err)
	}
	for _, email := range users {
		if err := CalibrateUserThreshold(ctx, email, defaultThreshold); err != nil {
			logger.Warnf("[FILTER] calibrate %s failed: %v", email, err)
		}
	}
	if err := store.DeleteFilterScoresBefore(ctx, since); err != nil {
		return fmt.Errorf("calibrate: prune: %w", err)
	}
	return nil
}

func CalibrateUserThreshold(ctx context.Context, email string, defaultThreshold float64) error {
	now := time.Now()
	rows, err := store.ListFilterScoresSince(ctx, email, now.AddDate(0, 0, -filterRetentionDays))
	if err != nil {
		return fmt.Errorf("calibrate %s: %w", email, err)
	}
	current := effectiveThreshold(ctx, email, defaultThreshold)
	next, ok := calibrationProposal(rows, current, now)
	if !ok {
		return nil
	}
	return proposeFilterThreshold(ctx, email, current, next, len(rows))
}

func proposeFilterThreshold(ctx context.Context, email string, current, next float64, evidence int) error {
	q := db.New(store.GetDB())
	pending, err := q.ListCorrectionObservationsByStatus(ctx, db.ListCorrectionObservationsByStatusParams{
		UserEmail: email, Status: "pending",
	})
	if err != nil {
		return fmt.Errorf("list pending observations: %w", err)
	}
	for _, o := range pending {
		if o.Kind == ObservationKindFilterThreshold {
			return nil
		}
	}
	from, to := fmt.Sprintf("%.2f", current), fmt.Sprintf("%.2f", next)
	_, err = q.GetCorrectionObservation(ctx, db.GetCorrectionObservationParams{
		UserEmail: email, Kind: ObservationKindFilterThreshold, FromValue: from, ToValue: to, Scope: filterCalibrationScope,
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("get filter threshold observation: %w", err)
	}
	if err := q.InsertCorrectionObservation(ctx, db.InsertCorrectionObservationParams{
		UserEmail: email, Kind: ObservationKindFilterThreshold, FromValue: from, ToValue: to,
		Scope: filterCalibrationScope, EvidenceCount: int64(evidence), SeenMessageIds: "[]", Status: "pending",
	}); err != nil {
		return fmt.Errorf("insert filter threshold observation: %w", err)
	}
	logger.Infof("[FILTER] %s: threshold proposal %s -> %s (%d scored rows)", email, from, to, evidence)
	return nil
}

// Why: only an approved decision may change the live threshold; the value is re-validated here
// because the observation row is plain text.
func applyApprovedFilterThreshold(ctx context.Context, email string, id int64) error {
	pending, err := db.New(store.GetDB()).ListCorrectionObservationsByStatus(ctx, db.ListCorrectionObservationsByStatusParams{
		UserEmail: email, Status: "pending",
	})
	if err != nil {
		return fmt.Errorf("list pending observations: %w", err)
	}
	for _, o := range pending {
		if o.ID != id || o.Kind != ObservationKindFilterThreshold {
			continue
		}
		v, ok := store.ParseFilterThreshold(o.ToValue)
		if !ok {
			return fmt.Errorf("filter threshold %q is outside [%.1f, %.1f]", o.ToValue, store.FilterThresholdMin, store.FilterThresholdMax)
		}
		if err := store.UpsertSetting(ctx, store.ClefThresholdKey(email), fmt.Sprintf("%.2f", v), email); err != nil {
			return fmt.Errorf("write threshold setting: %w", err)
		}
	}
	return nil
}
