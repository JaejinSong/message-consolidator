package scanner

import (
	"context"
	"message-consolidator/logger"
	"sync"
)

// reminderDispatcher decouples scanner from services package for test injection.
type reminderDispatcher interface {
	DispatchDueSoon(ctx context.Context) error
	DispatchUndated(ctx context.Context) error
	DispatchStalledReconfirm(ctx context.Context) error
}

func runDeadlineReminder(ctx context.Context, _ *sync.WaitGroup) {
	if deps.reminderSvc == nil {
		return
	}
	if !cfg.ReminderEnabled {
		return
	}
	if err := deps.reminderSvc.DispatchDueSoon(ctx); err != nil {
		logger.Warnf("[REMINDER] DispatchDueSoon failed: %v", err)
	}
	if err := deps.reminderSvc.DispatchUndated(ctx); err != nil {
		logger.Warnf("[REMINDER] DispatchUndated failed: %v", err)
	}
}

func runStalledReconfirm(ctx context.Context, _ *sync.WaitGroup) {
	if deps.reminderSvc == nil {
		return
	}
	if !cfg.ReminderEnabled {
		return
	}
	if err := deps.reminderSvc.DispatchStalledReconfirm(ctx); err != nil {
		logger.Warnf("[REMINDER] DispatchStalledReconfirm failed: %v", err)
	}
}
