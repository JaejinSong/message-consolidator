package scanner

import (
	"context"
	"errors"
	"message-consolidator/types"
	"testing"
)

// TestGroupOutcome covers the ack decision rules: noise-filtered and AI-success are
// always ok; enrichment failure is always retry; AI failure hinges on the envelope
// fallback producing at least one candidate.
func TestGroupOutcome(t *testing.T) {
	t.Parallel()
	errBoom := errors.New("boom")

	tests := []struct {
		name          string
		isNoise       bool
		enrichErr     error
		aiErr         error
		fallbackCount int
		want          bool
	}{
		{"noise-filtered is ok", true, nil, nil, 0, true},
		{"noise-filtered is ok even with a stale enrich err", true, errBoom, nil, 0, true},
		{"AI success (zero items) is ok", false, nil, nil, 0, true},
		{"enrichment failure is not ok", false, errBoom, nil, 0, false},
		{"AI error with zero fallback candidates is not ok", false, nil, errBoom, 0, false},
		{"AI error with >=1 fallback candidate is ok", false, nil, errBoom, 1, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := groupOutcome(tt.isNoise, tt.enrichErr, tt.aiErr, tt.fallbackCount); got != tt.want {
				t.Errorf("groupOutcome(%v, %v, %v, %d) = %v, want %v",
					tt.isNoise, tt.enrichErr, tt.aiErr, tt.fallbackCount, got, tt.want)
			}
		})
	}
}

// ackRecorder is a minimal ChannelAdapter that also implements scanAcker, so tests can
// assert exactly which ids/ok value the driver reports without any real channel logic.
type ackRecorderAdapter struct {
	whatsAppAdapter // embed for the unrelated ChannelAdapter methods this test never exercises
	calls           []ackCall
}

type ackCall struct {
	email string
	ids   []string
	ok    bool
}

func (a *ackRecorderAdapter) AckScanned(_ context.Context, email string, ids []string, ok bool) {
	a.calls = append(a.calls, ackCall{email: email, ids: ids, ok: ok})
}

func TestAckGroup_AdapterImplementsScanAcker(t *testing.T) {
	t.Parallel()
	adapter := &ackRecorderAdapter{}
	msgs := []types.RawMessage{{ID: "m1"}, {ID: "m2"}}

	ackGroup(context.Background(), adapter, "u@test.com", msgs, true)

	if len(adapter.calls) != 1 {
		t.Fatalf("AckScanned calls = %d, want 1", len(adapter.calls))
	}
	got := adapter.calls[0]
	if got.email != "u@test.com" || !got.ok {
		t.Errorf("call = %+v, want email=u@test.com ok=true", got)
	}
	if len(got.ids) != 2 || got.ids[0] != "m1" || got.ids[1] != "m2" {
		t.Errorf("ids = %v, want [m1 m2]", got.ids)
	}
}

// noAckAdapter implements ChannelAdapter but not scanAcker, mirroring Telegram/others.
type noAckAdapter struct {
	telegramAdapter
}

func TestAckGroup_AdapterWithoutScanAckerIsUnaffected(t *testing.T) {
	t.Parallel()
	adapter := noAckAdapter{}
	msgs := []types.RawMessage{{ID: "m1"}}

	// Why: nothing to assert on the adapter itself -- the test's real assertion is that
	// this does not panic on a missing AckScanned method (type assertion, not a call).
	ackGroup(context.Background(), adapter, "u@test.com", msgs, true)
}

func TestAckGroup_EmptyMessagesStillNotifiesWithEmptyIDs(t *testing.T) {
	t.Parallel()
	adapter := &ackRecorderAdapter{}

	ackGroup(context.Background(), adapter, "u@test.com", nil, false)

	if len(adapter.calls) != 1 {
		t.Fatalf("AckScanned calls = %d, want 1", len(adapter.calls))
	}
	if len(adapter.calls[0].ids) != 0 {
		t.Errorf("ids = %v, want empty", adapter.calls[0].ids)
	}
}

// TestWhatsAppAdapter_AckScanned_RoutesToStore documents the store call whatsAppAdapter
// makes for each ok value; it exercises the nil-DB guard path inside the store package
// (no real DB configured in this test binary) so it only asserts AckScanned does not panic
// and returns without a usable connection -- ok=true's routing to MarkWAMessagesProcessed
// is covered indirectly by store's own tests; ok=false is a no-op (scan_attempts was
// already bumped by MarkWAMessagesPopped).
func TestWhatsAppAdapter_AckScanned_DoesNotPanicWithoutDB(t *testing.T) {
	t.Parallel()
	adapter := whatsAppAdapter{}
	adapter.AckScanned(context.Background(), "u@test.com", []string{"m1"}, true)
	adapter.AckScanned(context.Background(), "u@test.com", []string{"m1"}, false)
}

