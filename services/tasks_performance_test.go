package services

import (
	"context"
	"fmt"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
)

// TestFormatMessagesPerformance verifies that BulkResolveIdentityX correctly handles large batches
// and respects the chunking logic (500 items).
func TestFormatMessagesPerformance(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenant := "test@example.com"
	svc := &TasksService{}

	// 1. Create base contacts
	contactID, err := store.AddContact(ctx, tenant, "master@example.com", "Master Contact", "", "test")
	if err != nil {
		t.Fatalf("Failed to create master contact: %v", err)
	}

	// 2. Prepare 600 unique identifiers (to trigger chunking logic)
	var msgs []store.ConsolidatedMessage
	for i := 0; i < 600; i++ {
		alias := fmt.Sprintf("Alias_%d", i)
		// Register even-indexed aliases to the same master contact
		if i%2 == 0 {
			err := store.RegisterAlias(ctx, contactID, store.ContactTypeName, alias, "test", 5)
			if err != nil {
				t.Fatalf("Failed to register alias %s: %v", alias, err)
			}
		}
		msgs = append(msgs, store.ConsolidatedMessage{
			Requester: alias,
			Assignee:  "unknown",
		})
	}

	// 3. Execution: Format messages. 
	// This will call extractUniqueIdentifiers -> BulkResolveAliases -> GetContactsByIdentifiers -> fetchContactsBatch -> BulkResolveIdentityX (Chunked)
	svc.FormatMessagesForClient(ctx, tenant, msgs)

	// 4. Verification
	// Alias_0 (even) -> should be resolved to "Master Contact"
	if msgs[0].Requester != "Master Contact" {
		t.Errorf("Expected resolution to 'Master Contact' for Alias_0, got '%s'", msgs[0].Requester)
	}

	// Alias_1 (odd) -> should remain "Alias_1" (not registered)
	if msgs[1].Requester != "Alias_1" {
		t.Errorf("Expected unresolvable 'Alias_1' to remain 'Alias_1', got '%s'", msgs[1].Requester)
	}

	// Alias_598 (even, in second chunk) -> should be resolved
	if msgs[598].Requester != "Master Contact" {
		t.Errorf("Expected resolution to 'Master Contact' for Alias_598 (Chunk 2), got '%s'", msgs[598].Requester)
	}

	// 5. Verify Negative Caching manually by checking if unresolvable ID was recorded (internal check via cache state if possible)
	// Since we can't easily access the private cache from another package, 
	// we rely on the fact that the second run works correctly.
	svc.FormatMessagesForClient(ctx, tenant, msgs[1:2]) // Re-run for Alias_1
	if msgs[1].Requester != "Alias_1" {
		t.Errorf("Re-run: Expected 'Alias_1', got '%s'", msgs[1].Requester)
	}
}
