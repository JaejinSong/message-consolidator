package channels

import (
	"fmt"
	"testing"
	"time"

	"message-consolidator/types"
)

func rawMsg(id string, ts time.Time) types.RawMessage {
	return types.RawMessage{ID: id, Timestamp: ts}
}

func TestChatBuffer_Replay(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("empty buffer", func(t *testing.T) {
		b := newChatBuffer()
		raws := []types.RawMessage{rawMsg("m1", base), rawMsg("m2", base.Add(time.Minute))}

		added := b.replay("u@x.com", "chat1", raws)

		if added != 2 {
			t.Fatalf("added = %d, want 2", added)
		}
		got := b.buf["u@x.com"]["chat1"]
		if len(got) != 2 || got[0].ID != "m1" || got[1].ID != "m2" {
			t.Fatalf("got = %+v", got)
		}
	})

	t.Run("nil and empty raws are no-ops", func(t *testing.T) {
		b := newChatBuffer()

		if added := b.replay("u@x.com", "chat1", nil); added != 0 {
			t.Fatalf("nil raws added = %d, want 0", added)
		}
		if added := b.replay("u@x.com", "chat1", []types.RawMessage{}); added != 0 {
			t.Fatalf("empty raws added = %d, want 0", added)
		}
		if _, ok := b.buf["u@x.com"]; ok {
			t.Fatalf("expected no map allocation side effect for empty input")
		}
	})

	t.Run("dedupe against existing live message", func(t *testing.T) {
		b := newChatBuffer()
		b.buffer("u@x.com", "chat1", rawMsg("live1", base.Add(time.Hour)))

		added := b.replay("u@x.com", "chat1", []types.RawMessage{rawMsg("live1", base), rawMsg("m2", base.Add(30*time.Minute))})

		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
		got := b.buf["u@x.com"]["chat1"]
		if len(got) != 2 {
			t.Fatalf("len(got) = %d, want 2", len(got))
		}
	})

	t.Run("dedupe within input", func(t *testing.T) {
		b := newChatBuffer()

		added := b.replay("u@x.com", "chat1", []types.RawMessage{
			rawMsg("m1", base),
			rawMsg("m1", base.Add(time.Minute)),
		})

		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
		got := b.buf["u@x.com"]["chat1"]
		if len(got) != 1 {
			t.Fatalf("len(got) = %d, want 1", len(got))
		}
	})

	t.Run("ordering by timestamp with live newer messages", func(t *testing.T) {
		b := newChatBuffer()
		b.buffer("u@x.com", "chat1", rawMsg("live1", base.Add(2*time.Hour)))
		b.buffer("u@x.com", "chat1", rawMsg("live2", base.Add(3*time.Hour)))

		b.replay("u@x.com", "chat1", []types.RawMessage{
			rawMsg("old2", base.Add(time.Hour)),
			rawMsg("old1", base),
		})

		got := b.buf["u@x.com"]["chat1"]
		wantOrder := []string{"old1", "old2", "live1", "live2"}
		if len(got) != len(wantOrder) {
			t.Fatalf("len(got) = %d, want %d", len(got), len(wantOrder))
		}
		for i, id := range wantOrder {
			if got[i].ID != id {
				t.Fatalf("got[%d].ID = %s, want %s", i, got[i].ID, id)
			}
		}
	})

	t.Run("cap trimming keeps newest 200", func(t *testing.T) {
		b := newChatBuffer()
		for i := 0; i < 150; i++ {
			b.buffer("u@x.com", "chat1", rawMsg(idOf("live", i), base.Add(time.Duration(1000+i)*time.Minute)))
		}
		raws := make([]types.RawMessage, 0, 100)
		for i := 0; i < 100; i++ {
			raws = append(raws, rawMsg(idOf("old", i), base.Add(time.Duration(i)*time.Minute)))
		}

		added := b.replay("u@x.com", "chat1", raws)

		if added != 100 {
			t.Fatalf("added = %d, want 100", added)
		}
		got := b.buf["u@x.com"]["chat1"]
		if len(got) != chatBufCap {
			t.Fatalf("len(got) = %d, want %d", len(got), chatBufCap)
		}
		// Oldest 50 replayed messages should have been trimmed; newest live messages retained.
		if got[0].ID != idOf("old", 50) {
			t.Fatalf("got[0].ID = %s, want %s", got[0].ID, idOf("old", 50))
		}
		if got[len(got)-1].ID != idOf("live", 149) {
			t.Fatalf("last ID = %s, want %s", got[len(got)-1].ID, idOf("live", 149))
		}
	})

	t.Run("pop after replay returns replayed messages", func(t *testing.T) {
		b := newChatBuffer()
		b.buffer("u@x.com", "chat1", rawMsg("live1", base.Add(time.Hour)))

		b.replay("u@x.com", "chat1", []types.RawMessage{rawMsg("old1", base)})

		popped := b.pop("u@x.com")
		got := popped["chat1"]
		if len(got) != 2 || got[0].ID != "old1" || got[1].ID != "live1" {
			t.Fatalf("got = %+v", got)
		}
	})
}

func idOf(prefix string, i int) string {
	return fmt.Sprintf("%s-%d", prefix, i)
}
