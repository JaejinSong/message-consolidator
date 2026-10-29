package main

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

type callRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *callRecorder) add(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
}

func (r *callRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

func (r *callRecorder) indexOf(name string) int {
	return slices.Index(r.snapshot(), name)
}

func newRecordedSteps(r *callRecorder, drainErr error, backgroundDone <-chan struct{}) shutdownSteps {
	return shutdownSteps{
		drainHTTP:         func(context.Context) error { r.add("drain"); return drainErr },
		cancelBackground:  func() { r.add("cancel") },
		backgroundDone:    backgroundDone,
		disconnectClients: func() { r.add("disconnect") },
		flush:             func() { r.add("flush") },
		closeDB:           func() { r.add("closeDB") },
	}
}

func closedChan() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func TestRunShutdown_RunsStepsInDocumentedOrder(t *testing.T) {
	// Arrange
	rec := &callRecorder{}
	done := make(chan struct{})
	steps := newRecordedSteps(rec, nil, done)
	steps.cancelBackground = func() { rec.add("cancel"); close(done) }

	// Act
	runShutdown(steps, time.Second)

	// Assert
	got := rec.snapshot()
	if len(got) != 5 {
		t.Fatalf("expected 5 calls, got %v", got)
	}
	if rec.indexOf("cancel") >= rec.indexOf("drain") {
		t.Errorf("cancel must precede drain: %v", got)
	}
	for _, parallel := range []string{"disconnect", "flush"} {
		if rec.indexOf("drain") > rec.indexOf(parallel) {
			t.Errorf("drain must precede %s: %v", parallel, got)
		}
		if rec.indexOf(parallel) > rec.indexOf("closeDB") {
			t.Errorf("%s must precede closeDB: %v", parallel, got)
		}
	}
	if got[len(got)-1] != "closeDB" {
		t.Errorf("closeDB must be last: %v", got)
	}
}

func TestRunShutdown_WaitsForBackgroundDoneBeforeDisconnecting(t *testing.T) {
	// Arrange
	rec := &callRecorder{}
	done := make(chan struct{})
	steps := newRecordedSteps(rec, nil, done)
	steps.cancelBackground = func() {
		rec.add("cancel")
		go func() {
			time.Sleep(100 * time.Millisecond)
			rec.add("background-finished")
			close(done)
		}()
	}

	// Act
	runShutdown(steps, 5*time.Second)

	// Assert
	finished := rec.indexOf("background-finished")
	if finished < 0 {
		t.Fatalf("background finish was never observed: %v", rec.snapshot())
	}
	for _, name := range []string{"disconnect", "flush", "closeDB"} {
		if rec.indexOf(name) < finished {
			t.Errorf("%s ran before background finished: %v", name, rec.snapshot())
		}
	}
}

func TestRunShutdown_ContinuesWhenBackgroundNeverFinishes(t *testing.T) {
	// Arrange
	rec := &callRecorder{}
	steps := newRecordedSteps(rec, nil, make(chan struct{}))

	// Act
	start := time.Now()
	runShutdown(steps, 50*time.Millisecond)

	// Assert
	for _, name := range []string{"disconnect", "flush", "closeDB"} {
		if rec.indexOf(name) < 0 {
			t.Errorf("%s was skipped after budget expiry: %v", name, rec.snapshot())
		}
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("shutdown took %v with a 50ms budget", elapsed)
	}
}

func TestRunShutdown_ContinuesWhenDrainFails(t *testing.T) {
	// Arrange
	rec := &callRecorder{}
	steps := newRecordedSteps(rec, errors.New("drain failed"), closedChan())

	// Act
	runShutdown(steps, time.Second)

	// Assert
	for _, name := range []string{"cancel", "disconnect", "flush", "closeDB"} {
		if rec.indexOf(name) < 0 {
			t.Errorf("%s was skipped after drain error: %v", name, rec.snapshot())
		}
	}
}

func TestRunShutdown_CancelsBackgroundBeforeDrainStarts(t *testing.T) {
	// Arrange
	rec := &callRecorder{}
	steps := newRecordedSteps(rec, nil, make(chan struct{}))
	var cancelledBeforeDrain bool
	steps.drainHTTP = func(ctx context.Context) error {
		cancelledBeforeDrain = rec.indexOf("cancel") >= 0
		<-ctx.Done()
		return ctx.Err()
	}

	// Act
	runShutdown(steps, 100*time.Millisecond)

	// Assert
	if !cancelledBeforeDrain {
		t.Error("cancelBackground must run before drainHTTP starts")
	}
	if rec.indexOf("closeDB") < 0 {
		t.Errorf("closeDB was skipped: %v", rec.snapshot())
	}
}
