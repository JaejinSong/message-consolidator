package services

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
	"time"
)

func TestAmbiguitySafeguard(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	tenant := "admin@whatap.io"
	
	// 1. 동일한 성(Lee)을 가진 서로 다른 두 명의 연락처 생성
	err = store.AddContactMapping(context.Background(), tenant, "lee1@whatap.io", "Lee Jung-jae", "Lee", "test")
	if err != nil {
		t.Fatalf("Failed to add contact 1: %v", err)
	}
	err = store.AddContactMapping(context.Background(), tenant, "lee2@whatap.io", "Lee Byung-hun", "Lee", "test")
	if err != nil {
		t.Fatalf("Failed to add contact 2: %v", err)
	}

	// 2. 모호한 별칭(Lee)을 사용하는 메시지 삽입
	msg := store.ConsolidatedMessage{
		UserEmail:  tenant,
		Source:     "slack",
		Room:       "room1",
		Task:       "Ambiguous Task",
		Requester:  "Lee",
		Assignee:   "someone@else.com",
		AssignedAt: time.Now(),
		SourceTS:   "999.999",
		Category:   "todo",
	}
	_, msgID, err := store.SaveMessage(context.TODO(), msg)
	if err != nil {
		t.Fatalf("Failed to save message: %v", err)
	}


	// 3. 서비스 초기화 및 정규화 실행
	svc := &ReportsService{}
	messages := []Log{
		{ID: msgID, Requester: "Lee", Assignee: "someone@else.com"},
	}
	svc.sanitizeMessages(context.Background(), tenant, messages)

	// 4. 리포트용 메모리 데이터 검증: (Ambiguous) 접미사 추가 확인
	if messages[0].Requester != "Lee (Ambiguous)" {
		t.Errorf("Expected 'Lee (Ambiguous)', got '%s'", messages[0].Requester)
	}

	// 5. DB 데이터 검증: 업데이트가 수행되지 않았어야 함 (원본 "Lee" 유지)
	// 비동기 업데이트 여부를 확인하기 위해 충분한 시간 대기
	time.Sleep(300 * time.Millisecond)
	
	db := store.GetDB()
	var dbReq string
	err = db.QueryRow("SELECT requester FROM messages WHERE id = ?", msgID).Scan(&dbReq)
	if err != nil {
		t.Fatalf("Failed to query DB: %v", err)
	}

	if dbReq != "Lee" {
		t.Errorf("DB requester should remain 'Lee' to prevent contamination, but got '%s'", dbReq)
	}
}
