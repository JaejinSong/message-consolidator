package scanner

import (
	"context"
	"sync"

	"message-consolidator/logger"
	"message-consolidator/services"
)

// runPrecisionObserver measures extraction precision from the user's own triage and
// records the worst buckets as rule proposals. Why it is safe to run unattended: the
// proposals use correction_observations kind='precision', which sits outside
// ListActiveSuppressRules' kind='suppress' filter, so nothing can apply them without a
// person promoting them first.
func runPrecisionObserver(ctx context.Context, _ *sync.WaitGroup) {
	if err := services.ObserveAllPrecision(ctx); err != nil {
		logger.Warnf("[PRECISION] ObserveAllPrecision failed: %v", err)
	}
}
