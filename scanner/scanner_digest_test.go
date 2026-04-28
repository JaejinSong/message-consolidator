package scanner

import (
	"context"
	"message-consolidator/config"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mockDigestDispatcher struct {
	callCount int
	mu        sync.Mutex
}

func (m *mockDigestDispatcher) Dispatch(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	return nil
}

func (m *mockDigestDispatcher) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func setupDigestTest(mock *mockDigestDispatcher, enabled bool, hour int) {
	digestLastSentDate = atomic.Value{}
	digestSvc = mock
	cfg = &config.Config{
		DailyDigestEnabled:  enabled,
		DailyDigestHour:     hour,
		DailyDigestTimezone: "Asia/Seoul",
	}
}

func kstTime(year, month, day, hour, minute, second int) time.Time {
	loc, _ := time.LoadLocation("Asia/Seoul")
	return time.Date(year, time.Month(month), day, hour, minute, second, 0, loc)
}

func TestRunDailyDigest_WeekdayAtHour_Dispatches(t *testing.T) {
	mock := &mockDigestDispatcher{}
	setupDigestTest(mock, true, 18)

	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 18, 0, 0) }

	runDailyDigest(context.Background(), nil)

	if mock.Count() != 1 {
		t.Errorf("expected 1 dispatch, got %d", mock.Count())
	}
	if last, _ := digestLastSentDate.Load().(string); last != "2026-04-28" {
		t.Errorf("expected lastSentDate=2026-04-28, got %q", last)
	}
}

func TestRunDailyDigest_SameDaySecondTick_NoDispatch(t *testing.T) {
	mock := &mockDigestDispatcher{}
	setupDigestTest(mock, true, 18)

	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 18, 0, 0) }
	runDailyDigest(context.Background(), nil)
	if mock.Count() != 1 {
		t.Fatalf("expected 1 dispatch after first tick, got %d", mock.Count())
	}

	// Why: second tick same day — dedup key must block re-dispatch.
	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 18, 1, 30) }
	runDailyDigest(context.Background(), nil)
	if mock.Count() != 1 {
		t.Errorf("expected still 1 dispatch, got %d", mock.Count())
	}
}

func TestRunDailyDigest_BeforeHour_NoDispatch(t *testing.T) {
	mock := &mockDigestDispatcher{}
	setupDigestTest(mock, true, 18)

	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 17, 59, 50) }
	runDailyDigest(context.Background(), nil)
	if mock.Count() != 0 {
		t.Errorf("expected 0 dispatches, got %d", mock.Count())
	}
}

func TestRunDailyDigest_PastMinute5_NoDispatch(t *testing.T) {
	mock := &mockDigestDispatcher{}
	setupDigestTest(mock, true, 18)

	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 18, 5, 0) }
	runDailyDigest(context.Background(), nil)
	if mock.Count() != 0 {
		t.Errorf("expected 0 dispatches, got %d", mock.Count())
	}
}

func TestRunDailyDigest_Saturday_NoDispatch(t *testing.T) {
	mock := &mockDigestDispatcher{}
	setupDigestTest(mock, true, 18)

	digestNowFn = func() time.Time {
		loc, _ := time.LoadLocation("Asia/Seoul")
		t := time.Date(2026, 5, 2, 18, 0, 0, 0, loc)
		if t.Weekday() != time.Saturday {
			panic("test date is not Saturday")
		}
		return t
	}
	runDailyDigest(context.Background(), nil)
	if mock.Count() != 0 {
		t.Errorf("expected 0 dispatches on Saturday, got %d", mock.Count())
	}
}

func TestRunDailyDigest_Disabled_NoDispatch(t *testing.T) {
	mock := &mockDigestDispatcher{}
	setupDigestTest(mock, false, 18)

	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 18, 0, 0) }
	runDailyDigest(context.Background(), nil)
	if mock.Count() != 0 {
		t.Errorf("expected 0 dispatches when disabled, got %d", mock.Count())
	}
}
