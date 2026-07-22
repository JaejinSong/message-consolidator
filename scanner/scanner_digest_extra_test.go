package scanner

import (
	"context"
	"errors"
	"message-consolidator/config"
	"sync/atomic"
	"testing"
	"time"
)

// fakeErrDispatcher is a dispatcher that always returns the configured error.
type fakeErrDispatcher struct {
	calls atomic.Int32
	err   error
}

func (f *fakeErrDispatcher) Dispatch(_ context.Context) error {
	f.calls.Add(1)
	return f.err
}

func TestTriggerDigest_NilSvc_ReturnsError(t *testing.T) {
	orig := deps.digestSvc
	t.Cleanup(func() { deps.digestSvc = orig })

	deps.digestSvc = nil

	if err := TriggerDigest(context.Background()); err == nil {
		t.Error("expected error when deps.digestSvc is nil, got nil")
	}
}

func TestTriggerDigest_HappyPath_CallsDispatch(t *testing.T) {
	orig := deps.digestSvc
	t.Cleanup(func() { deps.digestSvc = orig })

	fake := &fakeErrDispatcher{}
	deps.digestSvc = fake

	if err := TriggerDigest(context.Background()); err != nil {
		t.Errorf("TriggerDigest() unexpected error: %v", err)
	}
	if fake.calls.Load() != 1 {
		t.Errorf("Dispatch call count = %d, want 1", fake.calls.Load())
	}
}

func TestTriggerDigest_DispatchError_Propagates(t *testing.T) {
	orig := deps.digestSvc
	t.Cleanup(func() { deps.digestSvc = orig })

	want := errors.New("dispatch failed")
	fake := &fakeErrDispatcher{err: want}
	deps.digestSvc = fake

	if err := TriggerDigest(context.Background()); !errors.Is(err, want) {
		t.Errorf("TriggerDigest() error = %v, want %v", err, want)
	}
	if fake.calls.Load() != 1 {
		t.Errorf("Dispatch call count = %d, want 1", fake.calls.Load())
	}
}

func TestRunDailyDigest_NilSvc_NilCfg_NoDispatch(t *testing.T) {
	origSvc := deps.digestSvc
	origCfg := cfg
	t.Cleanup(func() {
		deps.digestSvc = origSvc
		cfg = origCfg
	})

	deps.digestSvc = nil
	cfg = nil

	// Should not panic.
	runDailyDigest(context.Background(), nil)
}

func TestRunDailyDigest_Sunday_NoDispatch(t *testing.T) {
	mock := &mockDigestDispatcher{}
	setupDigestTest(mock, true, 18)

	digestNowFn = func() time.Time {
		loc, _ := time.LoadLocation("Asia/Seoul")
		d := time.Date(2026, 5, 3, 18, 0, 0, 0, loc)
		if d.Weekday() != time.Sunday {
			panic("test date is not Sunday")
		}
		return d
	}
	runDailyDigest(context.Background(), nil)
	if mock.Count() != 0 {
		t.Errorf("expected 0 dispatches on Sunday, got %d", mock.Count())
	}
}

func TestRunDailyDigest_InvalidTimezone_NoDispatch(t *testing.T) {
	mock := &mockDigestDispatcher{}
	digestLastSentDate = atomic.Value{}
	origSvc := deps.digestSvc
	origCfg := cfg
	t.Cleanup(func() {
		deps.digestSvc = origSvc
		cfg = origCfg
	})

	deps.digestSvc = mock
	cfg = &config.Config{
		DailyDigestEnabled:  true,
		DailyDigestHour:     18,
		DailyDigestTimezone: "Invalid/Zone",
	}

	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 18, 0, 0) }
	runDailyDigest(context.Background(), nil)
	if mock.Count() != 0 {
		t.Errorf("expected 0 dispatches on invalid TZ, got %d", mock.Count())
	}
}

func TestRunDailyDigest_RetryInWindow_Dispatches(t *testing.T) {
	origNow := digestNowFn
	t.Cleanup(func() { digestNowFn = origNow })

	fake := &fakeErrDispatcher{err: errors.New("transient")}
	setupDigestTest(&mockDigestDispatcher{}, true, 18)
	deps.digestSvc = fake

	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 18, 5, 0) }
	runDailyDigest(context.Background(), nil)
	if fake.calls.Load() != 1 {
		t.Fatalf("expected dispatch at 18:xx, got %d", fake.calls.Load())
	}

	// Simulate next tick one hour later (19:xx) — still within retry window, date not stored.
	successMock := &mockDigestDispatcher{}
	deps.digestSvc = successMock
	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 19, 7, 0) }
	runDailyDigest(context.Background(), nil)
	if successMock.Count() != 1 {
		t.Errorf("expected retry dispatch at 19:xx, got %d", successMock.Count())
	}
}

func TestRunDailyDigest_OutsideRetryWindow_NoDispatch(t *testing.T) {
	origNow := digestNowFn
	t.Cleanup(func() { digestNowFn = origNow })

	fake := &fakeErrDispatcher{err: errors.New("transient")}
	setupDigestTest(&mockDigestDispatcher{}, true, 18)
	deps.digestSvc = fake

	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 18, 5, 0) }
	runDailyDigest(context.Background(), nil)

	noopMock := &mockDigestDispatcher{}
	deps.digestSvc = noopMock
	// 20:xx is outside the [18, 20) window.
	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 20, 3, 0) }
	runDailyDigest(context.Background(), nil)
	if noopMock.Count() != 0 {
		t.Errorf("expected no dispatch at 20:xx (outside window), got %d", noopMock.Count())
	}
}

func TestRunDailyDigest_AfterSuccessInWindow_NoRetry(t *testing.T) {
	origNow := digestNowFn
	t.Cleanup(func() { digestNowFn = origNow })

	mock := &mockDigestDispatcher{}
	setupDigestTest(mock, true, 18)

	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 18, 5, 0) }
	runDailyDigest(context.Background(), nil)
	if mock.Count() != 1 {
		t.Fatalf("expected 1 dispatch at 18:xx, got %d", mock.Count())
	}

	// 19:xx — still in window but already sent today.
	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 19, 7, 0) }
	runDailyDigest(context.Background(), nil)
	if mock.Count() != 1 {
		t.Errorf("expected no re-dispatch at 19:xx after success, got %d", mock.Count())
	}
}

func TestRunDailyDigest_DispatchError_NoStoreDateUpdate(t *testing.T) {
	origSvc := deps.digestSvc
	origCfg := cfg
	t.Cleanup(func() {
		deps.digestSvc = origSvc
		cfg = origCfg
		digestNowFn = func() time.Time { return time.Now() }
	})

	digestLastSentDate = atomic.Value{}
	fake := &fakeErrDispatcher{err: errors.New("dispatch failed")}
	deps.digestSvc = fake
	cfg = &config.Config{
		DailyDigestEnabled:  true,
		DailyDigestHour:     18,
		DailyDigestTimezone: "Asia/Seoul",
	}
	digestNowFn = func() time.Time { return kstTime(2026, 4, 28, 18, 0, 0) }

	runDailyDigest(context.Background(), nil)

	// Dispatch was called once.
	if fake.calls.Load() != 1 {
		t.Errorf("Dispatch call count = %d, want 1", fake.calls.Load())
	}
	// On dispatch failure the last-sent date must NOT be updated.
	if last, _ := digestLastSentDate.Load().(string); last != "" {
		t.Errorf("digestLastSentDate stored %q on error, want empty", last)
	}
}
