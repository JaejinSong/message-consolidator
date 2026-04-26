package scanner

import (
	"context"
	"message-consolidator/logger"
	"sync"
)

// reminderSvc is initialized in scanner.Init when SLACK_TOKEN is present.
// Nil-safe: runDeadlineReminder is a no-op if not configured.
var reminderSvc reminderDispatcher

// reminderDispatcher decouples scanner from services package for test injection.
type reminderDispatcher interface {
	DispatchDueSoon(ctx context.Context) error
}

func runDeadlineReminder(ctx context.Context, _ *sync.WaitGroup) {
	if reminderSvc == nil {
		return
	}
	if !cfg.ReminderEnabled {
		return
	}
	if err := reminderSvc.DispatchDueSoon(ctx); err != nil {
		logger.Warnf("[REMINDER] dispatch failed: %v", err)
	}
}
