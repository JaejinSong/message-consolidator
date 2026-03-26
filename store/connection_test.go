package store

import (
	"testing"
)

func TestDeleteGmailToken(t *testing.T) {
	cleanup, err := SetupTestDB()
	if err != nil {
		t.Fatalf("failed to setup test db: %v", err)
	}
	defer cleanup()

	email := "test@example.com"
	token := "ya29.test-token"

	// 1. 토큰 저장
	if err := SaveGmailToken(email, token); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	// 2. 저장 확인 (캐시 및 DB)
	has := HasGmailToken(email)
	if !has {
		t.Errorf("token should exist after save in cache")
	}

	// 3. 토큰 삭제
	if err := DeleteGmailToken(email); err != nil {
		t.Fatalf("failed to delete token: %v", err)
	}

	// 4. 삭제 확인
	has = HasGmailToken(email)
	if has {
		t.Errorf("token should not exist after delete in cache")
	}
}
