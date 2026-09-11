package handlers

import (
	"context"
	"testing"
	"time"
)

// TestTranslationContextSurvivesClientDisconnect pins the fix for the BatchTranslate failures.
// Why: HandleTranslate used to pass r.Context() straight into the chunked translation loop, so
// a browser that gave up cancelled the in-flight LLM call -- after its tokens were spent -- and
// the `break` on error dropped every remaining chunk with it. That showed up as a steady ~50%
// of BatchTranslate rows landing with source='failed' and "context canceled" in the log.
func TestTranslationContextSurvivesClientDisconnect(t *testing.T) {
	t.Parallel()
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := translationContext(parent)
	defer cancel()

	cancelParent() // the client walks away mid-translation
	if err := parent.Err(); err == nil {
		t.Fatal("parent ctx should be cancelled; the test premise is wrong")
	}
	if err := ctx.Err(); err != nil {
		t.Errorf("translation ctx = %v after the client disconnected, want it still live", err)
	}
}

// TestTranslationContextCarriesRequestValues guards the WhaTap trace linkage. Why: the cheap fix
// is context.Background(), which also drops the request's trace context and would split the
// translation work off its transaction. WithoutCancel keeps values and drops only cancellation.
func TestTranslationContextCarriesRequestValues(t *testing.T) {
	t.Parallel()
	type ctxKey struct{}
	parent := context.WithValue(context.Background(), ctxKey{}, "trace-abc")

	ctx, cancel := translationContext(parent)
	defer cancel()

	if got := ctx.Value(ctxKey{}); got != "trace-abc" {
		t.Errorf("ctx value = %v, want the request value carried through", got)
	}
}

// TestTranslationContextIsBounded keeps the detached loop from outliving the process. Why:
// dropping cancellation without a deadline turns a walked-away client into a goroutine that
// runs until the binary restarts.
func TestTranslationContextIsBounded(t *testing.T) {
	t.Parallel()
	ctx, cancel := translationContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("translation ctx has no deadline; a detached loop must still be bounded")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > translateBatchBudget {
		t.Errorf("deadline in %v, want within (0, %v]", remaining, translateBatchBudget)
	}
}
