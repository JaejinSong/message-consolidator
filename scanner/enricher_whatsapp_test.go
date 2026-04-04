package scanner

import (
	"fmt"
	"testing"
	"time"

	"message-consolidator/store"
)

func TestCalculateWindowStart(t *testing.T) {
	// Base time: 2024-01-01 12:07:00 (Unix: 1704110820)
	// Expect 12:00:00 (Unix: 1704110400)
	tm1 := time.Unix(1704110820, 0)
	got1 := calculateWindowStart(tm1)
	want1 := int64(1704110400)
	if got1 != want1 {
		t.Errorf("calculateWindowStart(%v) = %d; want %d", tm1, got1, want1)
	}

	// Base time: 2024-01-01 12:15:00 (Unix: 1704111300)
	// Expect 12:15:00 (Unix: 1704111300)
	tm2 := time.Unix(1704111300, 0)
	got2 := calculateWindowStart(tm2)
	want2 := int64(1704111300)
	if got2 != want2 {
		t.Errorf("calculateWindowStart(%v) = %d; want %d", tm2, got2, want2)
	}

	// Base time: 2024-01-01 12:29:59 (Unix: 1704112199)
	// Expect 12:15:00 (Unix: 1704111300)
	tm3 := time.Unix(1704112199, 0)
	got3 := calculateWindowStart(tm3)
	want3 := int64(1704111300)
	if got3 != want3 {
		t.Errorf("calculateWindowStart(%v) = %d; want %d", tm3, got3, want3)
	}
}

func TestEnrichWhatsAppMessage_Fallback(t *testing.T) {
	rawJID := "821012345678@s.whatsapp.net"
	msg := "Hello Task"
	timestamp := time.Now()

	// Use the newly defined AliasStore to satisfy the interface.
	as := &store.AliasStore{}
	enriched, err := EnrichWhatsAppMessage(rawJID, msg, timestamp, as)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if enriched.SenderID != 0 {
		t.Errorf("Expected SenderID 0 for fallback, got %d", enriched.SenderID)
	}

	if enriched.SenderName != rawJID {
		t.Errorf("Expected SenderName %s, got %s", rawJID, enriched.SenderName)
	}

	windowStart := (timestamp.Unix() / 900) * 900
	expectedThreadID := fmt.Sprintf("wa_thread_%s_%d", rawJID, windowStart)
	if enriched.VirtualThreadID != expectedThreadID {
		t.Errorf("Expected thread ID %s, got %s", expectedThreadID, enriched.VirtualThreadID)
	}
}
