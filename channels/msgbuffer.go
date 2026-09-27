package channels

import (
	"message-consolidator/types"
	"sort"
	"sync"
)

const chatBufCap = 200

// ChatBuffer is a per-email, per-chatKey circular message buffer.
type ChatBuffer struct {
	mu  sync.Mutex
	buf map[string]map[string][]types.RawMessage
}

func newChatBuffer() *ChatBuffer {
	return &ChatBuffer{buf: make(map[string]map[string][]types.RawMessage)}
}

func (b *ChatBuffer) buffer(email, chatKey string, raw types.RawMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buf[email] == nil {
		b.buf[email] = make(map[string][]types.RawMessage)
	}
	ch := append(b.buf[email][chatKey], raw)
	if len(ch) > chatBufCap {
		ch = ch[len(ch)-chatBufCap:]
	}
	b.buf[email][chatKey] = ch
}

// replay reinserts previously unbuffered messages (e.g. reconstructed from DB) into a
// chat's buffer, deduping by RawMessage.ID against both the input and the existing buffer,
// then re-sorts by Timestamp ascending and reapplies the cap. Returns the count actually added.
func (b *ChatBuffer) replay(email, chatKey string, raws []types.RawMessage) int {
	if len(raws) == 0 {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	existing := b.buf[email][chatKey]
	seen := make(map[string]struct{}, len(existing)+len(raws))
	for _, msg := range existing {
		seen[msg.ID] = struct{}{}
	}

	added := 0
	merged := existing
	for _, raw := range raws {
		if _, dup := seen[raw.ID]; dup {
			continue
		}
		seen[raw.ID] = struct{}{}
		merged = append(merged, raw)
		added++
	}
	if added == 0 {
		return 0
	}

	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Timestamp.Before(merged[j].Timestamp)
	})
	if len(merged) > chatBufCap {
		merged = merged[len(merged)-chatBufCap:]
	}

	if b.buf[email] == nil {
		b.buf[email] = make(map[string][]types.RawMessage)
	}
	b.buf[email][chatKey] = merged
	return added
}

func (b *ChatBuffer) pop(email string) map[string][]types.RawMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	userBuf, ok := b.buf[email]
	if !ok || len(userBuf) == 0 {
		return nil
	}
	out := make(map[string][]types.RawMessage, len(userBuf))
	for k, msgs := range userBuf {
		if len(msgs) > 0 {
			out[k] = msgs
		}
	}
	b.buf[email] = make(map[string][]types.RawMessage)
	return out
}

func (b *ChatBuffer) drop(email string) {
	b.mu.Lock()
	delete(b.buf, email)
	b.mu.Unlock()
}
