package scanner

import (
	"context"
	"message-consolidator/logger"
	"sync"
)

// exclusionDispatcher decouples scanner from services package for test injection.
type exclusionDispatcher interface {
	ProposeExclusionCandidates(ctx context.Context) error
	DispatchExcludedDigest(ctx context.Context) error
}

// runExclusionCandidate is chip-only (no DM), so it runs regardless of ReminderEnabled.
func runExclusionCandidate(ctx context.Context, _ *sync.WaitGroup) {
	if deps.exclusionSvc == nil {
		return
	}
	if err := deps.exclusionSvc.ProposeExclusionCandidates(ctx); err != nil {
		logger.Warnf("[EXCLUSION] ProposeExclusionCandidates failed: %v", err)
	}
}

func runExcludedDigest(ctx context.Context, _ *sync.WaitGroup) {
	if deps.exclusionSvc == nil {
		return
	}
	if !cfg.ReminderEnabled {
		return
	}
	if err := deps.exclusionSvc.DispatchExcludedDigest(ctx); err != nil {
		logger.Warnf("[EXCLUSION] DispatchExcludedDigest failed: %v", err)
	}
}
