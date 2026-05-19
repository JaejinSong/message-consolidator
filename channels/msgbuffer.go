package channels

import (
	"message-consolidator/types"
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
