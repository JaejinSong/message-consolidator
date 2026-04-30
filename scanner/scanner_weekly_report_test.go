package scanner

import (
	"context"
	"sync"
	"testing"
	"time"

	"message-consolidator/config"
)

type fakeWeeklyDispatcher struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeWeeklyDispatcher) Dispatch(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return nil
}

func (f *fakeWeeklyDispatcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// setupWeeklyTest wires package-level state and returns a cleanup func.
func setupWeeklyTest(t *testing.T, dispatcher weeklyReportDispatcher, nowFn func() time.Time) func() {
	t.Helper()
	origCfg := cfg
	origSvc := weeklyReportSvc
	origNowFn := weeklyReportNowFn
	weeklyReportLastSentDate.Store("")

	cfg = &config.Config{
		WeeklyReportEnabled:  true,
		WeeklyReportHour:     18,
		WeeklyReportTimezone: "Asia/Seoul",
	}
	weeklyReportSvc = dispatcher
	weeklyReportNowFn = nowFn

	return func() {
		cfg = origCfg
		weeklyReportSvc = origSvc
		weeklyReportNowFn = origNowFn
		weeklyReportLastSentDate.Store("")
	}
}

func fridayKST(hour, min int) time.Time {
	loc, _ := time.LoadLocation("Asia/Seoul")
	// 2026-05-01 is a Friday
	return time.Date(2026, 5, 1, hour, min, 0, 0, loc)
}

func TestRunWeeklyReport_Friday18_Dispatches(t *testing.T) {
	d := &fakeWeeklyDispatcher{}
	cleanup := setupWeeklyTest(t, d, func() time.Time { return fridayKST(18, 0) })
	defer cleanup()

	runWeeklyReport(context.Background(), nil)

	if d.count() != 1 {
		t.Errorf("want 1 dispatch, got %d", d.count())
	}
}

func TestRunWeeklyReport_Friday18_05_NoDispatch(t *testing.T) {
	d := &fakeWeeklyDispatcher{}
	cleanup := setupWeeklyTest(t, d, func() time.Time { return fridayKST(18, 5) })
	defer cleanup()

	runWeeklyReport(context.Background(), nil)

	if d.count() != 0 {
		t.Errorf("want 0 dispatch, got %d", d.count())
	}
}

func TestRunWeeklyReport_Friday19_NoDispatch(t *testing.T) {
	d := &fakeWeeklyDispatcher{}
	cleanup := setupWeeklyTest(t, d, func() time.Time { return fridayKST(19, 0) })
	defer cleanup()

	runWeeklyReport(context.Background(), nil)

	if d.count() != 0 {
		t.Errorf("want 0 dispatch, got %d", d.count())
	}
}

func TestRunWeeklyReport_Saturday18_NoDispatch(t *testing.T) {
	d := &fakeWeeklyDispatcher{}
	loc, _ := time.LoadLocation("Asia/Seoul")
	// 2026-05-02 is a Saturday
	saturday := time.Date(2026, 5, 2, 18, 0, 0, 0, loc)
	cleanup := setupWeeklyTest(t, d, func() time.Time { return saturday })
	defer cleanup()

	runWeeklyReport(context.Background(), nil)

	if d.count() != 0 {
		t.Errorf("want 0 dispatch, got %d", d.count())
	}
}

func TestRunWeeklyReport_Dedup(t *testing.T) {
	d := &fakeWeeklyDispatcher{}
	cleanup := setupWeeklyTest(t, d, func() time.Time { return fridayKST(18, 0) })
	defer cleanup()

	runWeeklyReport(context.Background(), nil)
	runWeeklyReport(context.Background(), nil)

	if d.count() != 1 {
		t.Errorf("want 1 dispatch (dedup), got %d", d.count())
	}
}

func TestRunWeeklyReport_Disabled_NoDispatch(t *testing.T) {
	d := &fakeWeeklyDispatcher{}
	cleanup := setupWeeklyTest(t, d, func() time.Time { return fridayKST(18, 0) })
	defer cleanup()
	cfg.WeeklyReportEnabled = false

	runWeeklyReport(context.Background(), nil)

	if d.count() != 0 {
		t.Errorf("want 0 dispatch when disabled, got %d", d.count())
	}
}
