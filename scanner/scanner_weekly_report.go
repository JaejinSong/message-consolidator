package scanner

import (
	"context"
	"message-consolidator/logger"
	"sync"
	"sync/atomic"
	"time"
)

var (
	weeklyReportSvc          weeklyReportDispatcher
	weeklyReportLastSentDate atomic.Value
	// Why: indirection for clock injection in tests; direct time.Now() blocks mocking.
	weeklyReportNowFn = func() time.Time { return time.Now() }
)

type weeklyReportDispatcher interface {
	Dispatch(ctx context.Context) error
}

func runWeeklyReport(ctx context.Context, _ *sync.WaitGroup) {
	if weeklyReportSvc == nil || cfg == nil || !cfg.WeeklyReportEnabled {
		return
	}
	loc, err := time.LoadLocation(cfg.WeeklyReportTimezone)
	if err != nil {
		logger.Warnf("[WEEKLY] invalid TZ %q: %v", cfg.WeeklyReportTimezone, err)
		return
	}
	now := weeklyReportNowFn().In(loc)
	if now.Weekday() != time.Friday {
		return
	}
	if now.Hour() != cfg.WeeklyReportHour || now.Minute() >= 5 {
		return
	}
	today := now.Format("2006-01-02")
	if last, _ := weeklyReportLastSentDate.Load().(string); last == today {
		return
	}
	if err := weeklyReportSvc.Dispatch(ctx); err != nil {
		logger.Warnf("[WEEKLY] dispatch failed: %v", err)
		return
	}
	weeklyReportLastSentDate.Store(today)
}
