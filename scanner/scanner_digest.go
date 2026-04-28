package scanner

import (
	"context"
	"message-consolidator/logger"
	"sync"
	"sync/atomic"
	"time"
)

var (
	digestSvc          digestDispatcher
	digestLastSentDate atomic.Value // Why: KST YYYY-MM-DD dedup key blocks repeat sends within the same day.
	// Why: indirection for clock injection in tests; direct time.Now() blocks mocking.
	digestNowFn = func() time.Time { return time.Now() }
)

// Why: scanner→services dependency inversion, mirroring reminderDispatcher in reminder_service.go.
type digestDispatcher interface {
	Dispatch(ctx context.Context) error
}

func runDailyDigest(ctx context.Context, _ *sync.WaitGroup) {
	if digestSvc == nil || cfg == nil || !cfg.DailyDigestEnabled {
		return
	}
	loc, err := time.LoadLocation(cfg.DailyDigestTimezone)
	if err != nil {
		logger.Warnf("[DIGEST] invalid TZ %q: %v", cfg.DailyDigestTimezone, err)
		return
	}
	now := digestNowFn().In(loc)
	if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday {
		return
	}
	if now.Hour() != cfg.DailyDigestHour || now.Minute() >= 5 {
		return
	}
	today := now.Format("2006-01-02")
	if last, _ := digestLastSentDate.Load().(string); last == today {
		return
	}
	if err := digestSvc.Dispatch(ctx); err != nil {
		logger.Warnf("[DIGEST] dispatch failed: %v", err)
		return
	}
	digestLastSentDate.Store(today)
}
