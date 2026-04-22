package services

import (
	"context"
	"fmt"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
)

// TestFormatMessagesPerformance verifies that BulkResolveAliases correctly handles large batches
// via the contact_resolution table.
func TestFormatMessagesPerformance(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenant := "test@example.com"
	svc := &TasksService{}

	// 1. Create 300 contacts for even-indexed identifiers (to trigger chunking at 500+)
	var msgs []store.ConsolidatedMessage
	for i := 0; i < 600; i++ {
		alias := fmt.Sprintf("alias_%d@example.com", i)
		if i%2 == 0 {
			_, err := store.AddContact(ctx, tenant, alias, fmt.Sprintf("Contact %d", i), "", "test")
			if err != nil {
				t.Fatalf("Failed to create contact %s: %v", alias, err)
			}
		}
		msgs = append(msgs, store.ConsolidatedMessage{
			Requester: alias,
			Assignee:  "unknown",
		})
	}

	// 2. Execution: triggers extractUniqueIdentifiers → BulkResolveAliases → contact_resolution table lookup
	svc.FormatMessagesForClient(ctx, tenant, msgs)

	// 3. Verification
	// alias_0 (even, contact exists) -> resolved to display name
	if msgs[0].Requester != "Contact 0" {
		t.Errorf("Expected resolution to 'Contact 0' for alias_0, got '%s'", msgs[0].Requester)
	}

	// alias_1@example.com (odd, no contact) -> remains as-is
	if msgs[1].Requester != "alias_1@example.com" {
		t.Errorf("Expected unresolvable alias to remain, got '%s'", msgs[1].Requester)
	}

	// alias_598 (even, in second chunk) -> resolved
	if msgs[598].Requester != "Contact 598" {
		t.Errorf("Expected resolution for alias_598 (Chunk 2), got '%s'", msgs[598].Requester)
	}

	// 4. Re-run for unresolvable (negative cache check)
	svc.FormatMessagesForClient(ctx, tenant, msgs[1:2])
	if msgs[1].Requester != "alias_1@example.com" {
		t.Errorf("Re-run: Expected 'alias_1@example.com', got '%s'", msgs[1].Requester)
	}
}
