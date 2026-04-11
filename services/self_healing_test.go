package services

import (
	"context"
	"fmt"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
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
	alias := "JJ" + testutil.RandomID("")

	// 1. Setup Contact (Deep Lookup 대상)
	err = store.AddContactMapping(context.Background(), tenant, email, name, alias, "test")
	if err != nil {
		t.Fatalf("Failed to add contact: %v", err)
	}

	// 2. Insert fragmented message (Requester가 별칭인 "JJ"로 저장됨)
	msg := store.ConsolidatedMessage{
		UserEmail:  tenant,
		Source:     "slack",
		Room:       "room1",
		Task:       "Test Task",
		Requester:  alias,
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
		{ID: msgID, UserEmail: tenant, Room: "room1", Requester: alias, Assignee: "someone@else.com"},
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
