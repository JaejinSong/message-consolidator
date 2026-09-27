package services

import (
	"message-consolidator/types"
	"testing"
)

func TestFanOutWAReceivers(t *testing.T) {
	var order []int
	receive := FanOutWAReceivers(
		func(email, chatJID string, msg types.RawMessage) { order = append(order, 1) },
		func(email, chatJID string, msg types.RawMessage) { order = append(order, 2) },
	)

	receive("a@b.com", "chat1", types.RawMessage{ID: "m1"})

	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("expected receivers called in order [1 2], got %v", order)
	}
}

func TestFanOutWAReceivers_AllReceiveSameMessage(t *testing.T) {
	var got1, got2 string // captured msg.ID from each receiver
	var email1, email2, chat1, chat2 string
	receive := FanOutWAReceivers(
		func(email, chatJID string, msg types.RawMessage) { email1, chat1, got1 = email, chatJID, msg.ID },
		func(email, chatJID string, msg types.RawMessage) { email2, chat2, got2 = email, chatJID, msg.ID },
	)

	want := types.RawMessage{ID: "m2", Text: "hello"}
	receive("u@x.com", "chatJID", want)

	if email1 != email2 || chat1 != chat2 || got1 != got2 {
		t.Fatalf("expected all receivers to observe identical args, got (%s,%s,%s) vs (%s,%s,%s)",
			email1, chat1, got1, email2, chat2, got2)
	}
	if got1 != want.ID {
		t.Fatalf("expected message ID %s, got %s", want.ID, got1)
	}
}
