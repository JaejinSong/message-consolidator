package scanner

import (
	"context"
	"testing"
)

type dispatchCtxTraceKey struct{}

// TestDetachedDispatchCtx verifies detachedDispatchCtx keeps a completion-dispatch
// goroutine alive past the parent scan ctx's cancellation while still carrying the
// parent's trace value, and bounds the goroutine with its own deadline.
func TestDetachedDispatchCtx(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	parent = context.WithValue(parent, dispatchCtxTraceKey{}, "trace-123")

	dispatchCtx, cancel := detachedDispatchCtx(parent)
	defer cancel()

	parentCancel()
	if err := dispatchCtx.Err(); err != nil {
		t.Fatalf("dispatchCtx cancelled when parent scan ctx was cancelled: %v", err)
	}

	if got := dispatchCtx.Value(dispatchCtxTraceKey{}); got != "trace-123" {
		t.Fatalf("dispatchCtx did not carry parent trace value, got %v", got)
	}

	if _, ok := dispatchCtx.Deadline(); !ok {
		t.Fatal("dispatchCtx has no deadline")
	}

	cancel()
	if err := dispatchCtx.Err(); err != context.Canceled {
		t.Fatalf("dispatchCtx not cancelled after calling cancel(), err = %v", err)
	}
}
