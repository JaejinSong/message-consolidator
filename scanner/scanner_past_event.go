package scanner

import (
	"context"
	"sync"
	"time"

	"message-consolidator/logger"
	"message-consolidator/store"
)

// pastEventDispatcher decouples scanner from services package for test injection.
type pastEventDispatcher interface {
	ProposePastEventCandidates(ctx context.Context, email string, now time.Time) (int, error)
}

// runPastEventCandidate is chip-only (no DM), so it runs regardless of ReminderEnabled.
func runPastEventCandidate(ctx context.Context, _ *sync.WaitGroup) {
	if deps.pastEventSvc == nil {
		return
	}
	users, err := store.GetAllUsers(ctx)
	if err != nil {
		logger.Warnf("[PAST-EVENT] get all users failed: %v", err)
		return
	}
	now := time.Now()
	for _, u := range users {
		if _, err := deps.pastEventSvc.ProposePastEventCandidates(ctx, u.Email, now); err != nil {
			logger.Warnf("[PAST-EVENT] propose failed user=%s: %v", u.Email, err)
		}
	}
}
