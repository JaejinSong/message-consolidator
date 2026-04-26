package scanner

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrimePool_OnlyContainsExpectedPrimes(t *testing.T) {
	expected := map[time.Duration]struct{}{
		59 * time.Second: {},
		61 * time.Second: {},
		67 * time.Second: {},
		71 * time.Second: {},
		73 * time.Second: {},
	}
	if len(primePool) != len(expected) {
		t.Fatalf("primePool size=%d want=%d", len(primePool), len(expected))
	}
	for _, p := range primePool {
		if _, ok := expected[p]; !ok {
			t.Errorf("unexpected prime %s in primePool", p)
		}
	}
}

func TestPickPrime_CoversWholePool(t *testing.T) {
	seen := make(map[time.Duration]int)
	for i := 0; i < 1000; i++ {
		p := pickPrime()
		seen[p]++
	}
	if len(seen) != len(primePool) {
		t.Fatalf("pickPrime hit %d distinct values in 1000 calls; want %d (1/4^1000 chance of false fail)", len(seen), len(primePool))
	}
	for _, p := range primePool {
		if seen[p] == 0 {
			t.Errorf("prime %s never selected", p)
		}
	}
}

// Why: Guards extensibility — adding new primes (e.g. 79s, 83s) should propagate through pickPrime without code changes.
func TestPickPrime_PoolExtensionPropagates(t *testing.T) {
	original := primePool
	t.Cleanup(func() { primePool = original })

	primePool = append([]time.Duration{}, original...)
	primePool = append(primePool, 79*time.Second, 83*time.Second)

	hit79, hit83 := false, false
	for i := 0; i < 2000 && !(hit79 && hit83); i++ {
		switch pickPrime() {
		case 79 * time.Second:
			hit79 = true
		case 83 * time.Second:
			hit83 = true
		}
	}
	if !hit79 || !hit83 {
		t.Fatalf("extended pool not reflected: 79s=%v 83s=%v", hit79, hit83)
	}
}

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
