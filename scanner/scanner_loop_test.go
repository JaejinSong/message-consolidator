package scanner

import (
	"context"
	"message-consolidator/internal/primes"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrimeLoop_TickSkipsWhenAlreadyRunning(t *testing.T) {
	var inflight atomic.Int32
	var maxInflight atomic.Int32
	release := make(chan struct{})

	loop := &primeLoop{
		name:      "test",
		traceName: "/Background-Test",
		runFn: func(ctx context.Context, _ *sync.WaitGroup) {
			n := inflight.Add(1)
			defer inflight.Add(-1)
			if n > maxInflight.Load() {
				maxInflight.Store(n)
			}
			<-release
		},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		loop.tick(context.Background(), &sync.WaitGroup{})
	}()

	// Why: spin until first tick has acquired the running flag, otherwise the second tick may race ahead.
	for inflight.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	loop.tick(context.Background(), &sync.WaitGroup{}) // should be rejected by CAS
	if maxInflight.Load() != 1 {
		t.Fatalf("expected at most 1 concurrent run, observed %d", maxInflight.Load())
	}

	close(release)
	wg.Wait()
}

func TestPrimeLoop_StartHonorsContextCancellation(t *testing.T) {
	var ticks atomic.Int32
	loop := &primeLoop{
		name:      "test-cancel",
		traceName: "/Background-TestCancel",
		runFn: func(_ context.Context, _ *sync.WaitGroup) {
			ticks.Add(1)
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go loop.start(ctx, &wg, time.Hour) // long enough that only the immediate first run fires

	// Why: wait for the immediate first tick (start() runs tick once before arming the timer).
	for ticks.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("primeLoop.start did not exit after context cancellation")
	}

	if ticks.Load() != 1 {
		t.Errorf("expected exactly 1 tick (the immediate startup run), got %d", ticks.Load())
	}
}

func TestHourPrimePool_OnlyContainsExpectedPrimes(t *testing.T) {
	expected := map[time.Duration]struct{}{
		3557 * time.Second: {},
		3571 * time.Second: {},
		3593 * time.Second: {},
		3607 * time.Second: {},
		3613 * time.Second: {},
	}
	if len(hourPrimePool) != len(expected) {
		t.Fatalf("hourPrimePool size=%d want=%d", len(hourPrimePool), len(expected))
	}
	for _, p := range hourPrimePool {
		if _, ok := expected[p]; !ok {
			t.Errorf("unexpected value %s in hourPrimePool", p)
		}
	}
}

func TestPrimeLoop_PickNext_UsesOwnPool(t *testing.T) {
	onlyVal := 37 * time.Minute
	loop := &primeLoop{pool: []time.Duration{onlyVal}}
	for i := 0; i < 50; i++ {
		if got := loop.pickNext(); got != onlyVal {
			t.Fatalf("pickNext returned %s, want %s", got, onlyVal)
		}
	}
}

func TestPrimeLoop_PickNext_FallsBackToGlobal(t *testing.T) {
	loop := &primeLoop{} // pool == nil → falls back to primes.Seconds
	got := loop.pickNext()
	found := false
	for _, p := range primes.Seconds {
		if got == p {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("pickNext returned %s which is not in primes.Seconds", got)
	}
}
