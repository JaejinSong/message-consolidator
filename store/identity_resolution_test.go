package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestIdentityResolutionViews(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	tenantEmail := testutil.RandomEmail("idres")

	// 1. Create Contacts (Master & Child)
	masterEmail := testutil.RandomEmail("master")
	childEmail := testutil.RandomEmail("minion")
	masterID, _ := AddContact(context.Background(), tenantEmail, masterEmail, "The Big Boss", "", "gmail")
	childID, _ := AddContact(context.Background(), tenantEmail, childEmail, "Poor Minion", "", "whatsapp")
	
	_ = LinkContact(context.Background(), tenantEmail, masterID, childID)

	t.Run("v_contacts_resolved", func(t *testing.T) {
		// Child 계정 조회 시 Master의 정보가 나오는지 확인
		var effectiveName, effectiveCanonical string
		err := db.QueryRow("SELECT effective_display_name, effective_canonical_id FROM v_contacts_resolved WHERE id = ?", childID).
			Scan(&effectiveName, &effectiveCanonical)
		
		if err != nil {
			t.Fatalf("Failed to query v_contacts_resolved: %v", err)
		}
		
		if effectiveName != "The Big Boss" {
			t.Errorf("Expected master name 'The Big Boss', got '%s'", effectiveName)
		}
		if effectiveCanonical != masterEmail {
			t.Errorf("Expected master canonical '%s', got '%s'", masterEmail, effectiveCanonical)
		}
	})

	t.Run("v_messages integration", func(t *testing.T) {
		sourceTS := testutil.RandomTS("ts_id_res")
		_, err := db.Exec("INSERT INTO messages (user_email, task, source, requester, assignee, source_ts) VALUES (?, ?, ?, ?, ?, ?)",
			tenantEmail, "Urgent Task", "whatsapp", childEmail, masterEmail, sourceTS)
		if err != nil {
			t.Fatalf("Failed to insert test message: %v", err)
		}

		// v_messages 조회 시 requester 이름이 'The Big Boss'로 해소되었는지 확인
		var requesterName string
		err = db.QueryRow("SELECT requester FROM v_messages WHERE user_email = ? AND source_ts = ?", tenantEmail, sourceTS).
			Scan(&requesterName)
		
		if err != nil {
			t.Fatalf("Failed to query v_messages: %v", err)
		}

		if requesterName != "The Big Boss" {
			t.Errorf("Expected resolved requester name 'The Big Boss', got '%s'", requesterName)
		}
	})
}
