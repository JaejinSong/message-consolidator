package scanner

import (
	"context"
	"math/rand/v2"
	"message-consolidator/logger"
	"sync"
	"sync/atomic"
	"time"

	"github.com/whatap/go-api/trace"
)

// Why: Prime-second pool diversifies background loop cadence vs 1-minute upstream cron/poller.
// Extending (e.g. 79s) requires only editing this slice; pickPrime/primeLoop pick up automatically.
var primePool = []time.Duration{
	59 * time.Second,
	61 * time.Second,
	67 * time.Second,
	71 * time.Second,
	73 * time.Second,
}

func pickPrime() time.Duration {
	// #nosec G404 -- Scheduling jitter, not security: math/rand/v2 is the right choice.
	return primePool[rand.IntN(len(primePool))]
}

// primeLoop drives a single periodic worker. Every tick re-rolls its next interval from primePool;
// running guard skips overlapping ticks (long scan still in flight).
type primeLoop struct {
	name      string
	traceName string
	runFn     func(ctx context.Context, wg *sync.WaitGroup)
	running   atomic.Bool
}

func (l *primeLoop) tick(ctx context.Context, wg *sync.WaitGroup) {
	if !l.running.CompareAndSwap(false, true) {
		logger.Warnf("[%s] previous run still in flight, skipping tick", l.name)
		return
	}
	defer l.running.Store(false)

	traceCtx, _ := trace.Start(ctx, l.traceName)
	defer func() { _ = trace.End(traceCtx, nil) }()
	l.runFn(traceCtx, wg)
}

func (l *primeLoop) start(ctx context.Context, wg *sync.WaitGroup, first time.Duration) {
	defer wg.Done()

	// Why: Immediate first run preserves the legacy startup behavior where dashboards populate without waiting.
	l.tick(ctx, wg)

	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			l.tick(ctx, wg)
			timer.Reset(pickPrime())
		}
	}
}
