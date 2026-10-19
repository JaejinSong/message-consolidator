package scanner

import (
	"context"
	"sync"

	"message-consolidator/logger"
	"message-consolidator/services"
)

// Why: proposals are confirm-first (correction_observations kind='filter_threshold'), so the
// unattended run never changes a live threshold; it also prunes filter_scores past retention.
func runFilterCalibrator(ctx context.Context, _ *sync.WaitGroup) {
	if cfg == nil || cfg.FilterProvider != "clef" {
		return
	}
	if err := services.CalibrateAllFilterThresholds(ctx, cfg.ClefThreshold); err != nil {
		logger.Warnf("[FILTER] calibrator failed: %v", err)
	}
}
