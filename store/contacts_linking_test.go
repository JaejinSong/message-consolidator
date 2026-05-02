package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestLinkContact(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	tenantEmail := "test@example.com"

	// Setup initial contacts
	masterID, err := AddContact(context.Background(), tenantEmail, "master@gmail.com", "Master User", "", "gmail")
	if err != nil {
		t.Fatalf("Failed to create master: %v", err)
	}
	childID, err := AddContact(context.Background(), tenantEmail, "child@whatsapp", "Child User", "", "whatsapp")
	if err != nil {
		t.Fatalf("Failed to create child: %v", err)
	}

	t.Run("Basic Linking", func(t *testing.T) {
		err := LinkContact(context.Background(), tenantEmail, masterID, childID)
		if err != nil {
			t.Errorf("LinkContact failed: %v", err)
		}

		child, _ := GetContactByID(context.Background(), tenantEmail, childID)
		if !child.MasterContactID.Valid || child.MasterContactID.Int64 != masterID {
			t.Errorf("Expected child to be linked to master %d, got %v", masterID, child.MasterContactID)
		}
	})

	t.Run("Tree Flattening (1-Level Hierarchy)", func(t *testing.T) {
		// Scenario: masterID <- childID 이미 연결됨.
		// 새로운 rootID를 만들고 masterID를 rootID에 연결.
		rootID, _ := AddContact(context.Background(), tenantEmail, "root@boss.com", "Root Boss", "", "gmail")

		err := LinkContact(context.Background(), tenantEmail, rootID, masterID)
		if err != nil {
			t.Fatalf("Failed to link master to root: %v", err)
		}

		// 검증: masterID -> rootID
		m, _ := GetContactByID(context.Background(), tenantEmail, masterID)
		if m.MasterContactID.Int64 != rootID {
			t.Errorf("Master should now point to Root %d", rootID)
		}

		// 검증: childID -> rootID (원래 masterID였으나 Flattening됨)
		c, _ := GetContactByID(context.Background(), tenantEmail, childID)
		if c.MasterContactID.Int64 != rootID {
			t.Errorf("Child should have been flattened to Root %d, but points to %v", rootID, c.MasterContactID)
		}
	})

	t.Run("Unlinking", func(t *testing.T) {
		err := UnlinkContact(context.Background(), tenantEmail, childID)
		if err != nil {
			t.Errorf("UnlinkContact failed: %v", err)
		}

		c, _ := GetContactByID(context.Background(), tenantEmail, childID)
		if c.MasterContactID.Valid {
			t.Errorf("Child should be unlinked (Valid=false), got %v", c.MasterContactID)
		}
	})

	t.Run("Circular Reference Prevention - Self", func(t *testing.T) {
		err := LinkContact(context.Background(), tenantEmail, masterID, masterID)
		if err == nil {
			t.Error("Expected error when linking to self, got nil")
		}
	})

	t.Run("Circular Reference Prevention - Child to Master", func(t *testing.T) {
		// rootID <- masterID 상태 (위의 flattening 테스트 결과)
		// masterID를 master로 하고 rootID를 자식으로 연결 시도 (역방향)
		rootID, _ := GetContactByIdentifier(context.Background(), tenantEmail, "root@boss.com")
		if rootID == nil {
			t.Fatalf("Failed to resolve root@boss.com")
		}

		err := LinkContact(context.Background(), tenantEmail, masterID, rootID.ID)
		if err == nil {
			t.Error("Expected circular reference error, got nil")
		}
	})
}
