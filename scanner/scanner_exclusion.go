package scanner

import (
	"context"
	"message-consolidator/logger"
	"sync"
)

// exclusionSvc is initialized in scanner.Init. Nil-safe: loops no-op if not configured.
var exclusionSvc exclusionDispatcher

// exclusionDispatcher decouples scanner from services package for test injection.
type exclusionDispatcher interface {
	ProposeExclusionCandidates(ctx context.Context) error
	DispatchExcludedDigest(ctx context.Context) error
}

// runExclusionCandidate is chip-only (no DM), so it runs regardless of ReminderEnabled.
func runExclusionCandidate(ctx context.Context, _ *sync.WaitGroup) {
	if exclusionSvc == nil {
		return
	}
	if err := exclusionSvc.ProposeExclusionCandidates(ctx); err != nil {
		logger.Warnf("[EXCLUSION] ProposeExclusionCandidates failed: %v", err)
	}
}

func runExcludedDigest(ctx context.Context, _ *sync.WaitGroup) {
	if exclusionSvc == nil {
		return
	}
	if !cfg.ReminderEnabled {
		return
	}
	if err := exclusionSvc.DispatchExcludedDigest(ctx); err != nil {
		logger.Warnf("[EXCLUSION] DispatchExcludedDigest failed: %v", err)
	}
}
