package services

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
	"time"
)

func TestSelfHealingEngine(t *testing.T) {
	cleanup, err := testutil.SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	tenant := "admin@whatap.io"
	email := "jjsong@whatap.io"
	name := "Jaejin Song"
	alias := "JJ"

	// 1. Setup Contact (Deep Lookup 대상)
	err = store.AddContactMapping(tenant, email, name, alias, "test")
	if err != nil {
		t.Fatalf("Failed to add contact: %v", err)
	}

	// 2. Insert fragmented message (Requester가 별칭인 "JJ"로 저장됨)
	msg := store.ConsolidatedMessage{
		UserEmail:  tenant,
		Source:     "slack",
		Room:       "room1",
		Task:       "Test Task",
		Requester:  "JJ",
		Assignee:   "someone@else.com",
		AssignedAt: time.Now(),
		SourceTS:   "12345.678",
		Category:   "todo",
	}
	_, msgID, err := store.SaveMessage(msg)
	if err != nil {
		t.Fatalf("Failed to save message: %v", err)
	}

	// 3. Initialize Service
	svc := &ReportsService{}

	// 4. Run Sanitization
	messages := []Log{
		{ID: msgID, Requester: "JJ", Assignee: "someone@else.com"},
	}
	svc.sanitizeMessages(context.Background(), tenant, messages)

	// 5. Verify In-Memory Update (즉시 반영 확인)
	if messages[0].Requester != email {
		t.Errorf("In-memory requester not healed. Expected %s, got %s", email, messages[0].Requester)
	}

	// 6. Verify DB Update (비동기 고루틴 실행 대기)
	time.Sleep(200 * time.Millisecond) // Goroutine 대기
	
	db := store.GetDB()
	var dbReq string
	err = db.QueryRow("SELECT requester FROM messages WHERE id = ?", msgID).Scan(&dbReq)
	if err != nil {
		t.Fatalf("Failed to query DB: %v", err)
	}

	if dbReq != email {
		t.Errorf("DB requester not healed. Expected %s, got %s", email, dbReq)
	}
}
