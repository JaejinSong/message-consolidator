package services

import (
	"context"
	"fmt"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"strings"
	"testing"
	"time"
)

func TestSelfHealingEngine(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	tenant := testutil.RandomEmail("admin")
	email := testutil.RandomEmail("jjsong")
	name := "Jaejin Song"

	// 1. Setup Contact (Deep Lookup 대상)
	err = store.AddContactMapping(context.Background(), tenant, email, name, "", "test")
	if err != nil {
		t.Fatalf("Failed to add contact: %v", err)
	}

	// 2. Insert fragmented message (Requester가 대소문자 다른 display_name으로 저장됨 — healing 트리거 조건)
	requester := strings.ToUpper(name) // "JAEJIN SONG" — resolves via case-insensitive lookup but triggers DB heal
	msg := store.ConsolidatedMessage{
		UserEmail:  tenant,
		Source:     "slack",
		Room:       "room1",
		Task:       "Test Task",
		Requester:  requester,
		Assignee:   "someone@else.com",
		AssignedAt: time.Now(),
		SourceTS:   fmt.Sprintf("12345.678.%d", time.Now().UnixNano()),
		Category:   "todo",
	}
	_, msgID, err := store.SaveMessage(context.TODO(), store.GetDB(), msg)
	if err != nil {
		t.Fatalf("Failed to save message: %v", err)
	}

	// 3. Initialize Service
	svc := &ReportsService{}

	// 4. Run Sanitization
	messages := []Log{
		{ID: msgID, UserEmail: tenant, Room: "room1", Requester: requester, Assignee: "someone@else.com"},
	}
	svc.sanitizeMessages(context.Background(), tenant, messages)

	// 5. Verify In-Memory Update — sanitizeMessages resolves for display without writing to DB.
	if messages[0].Requester != email {
		t.Errorf("in-memory requester not resolved: expected %s, got %s", email, messages[0].Requester)
	}

	// 6. Verify DB is NOT modified by sanitizeMessages — read path must have no write side-effects.
	// SaveMessage already normalizes the requester (e.g. "JAEJIN SONG" → "Jaejin Song"),
	// but sanitizeMessages must not further rewrite it to a canonical email.
	db := store.GetDB()
	var dbReq string
	if err = db.QueryRow("SELECT requester FROM messages WHERE id = ?", msgID).Scan(&dbReq); err != nil {
		t.Fatalf("Failed to query DB: %v", err)
	}
	if dbReq == email {
		t.Errorf("sanitizeMessages must not rewrite requester to canonical email %q in DB", email)
	}
}
