package services

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
)

func TestTaskClassificationByAliases(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "jj@whatap.io"
	name := "Jaejin Song"

	// 1. Setup User and Aliases
	user, err := store.GetOrCreateUser(ctx, email, name, "")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	store.AddUserAlias(ctx, user.ID, "송재진")
	store.AddUserAlias(ctx, user.ID, "jj")

	service := &TasksService{}

	tests := []struct {
		name     string
		assignee string
		expectedCategory string
		expectedAssignee string
	}{
		{"Exact Name Match", "Jaejin Song", CategoryPersonal, "me"},
		{"Alias Match (Korean)", "송재진", CategoryPersonal, "me"},
		{"Alias Match (Short)", "jj", CategoryPersonal, "me"},
		{"Token Match", "__CURRENT_USER__", CategoryPersonal, "me"},
		{"Literal 'me'", "me", CategoryPersonal, "me"},
		{"Non-match", "Other Person", CategoryOthers, "Other Person"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &store.ConsolidatedMessage{
				Assignee: tt.assignee,
			}
			service.applyAssigneeRules(ctx, user, msg)
			service.assignCategory(email, user, msg)

			if msg.Category != tt.expectedCategory {
				t.Errorf("expected category %s, got %s", tt.expectedCategory, msg.Category)
			}
			if msg.Assignee != tt.expectedAssignee {
				t.Errorf("expected assignee %s, got %s", tt.expectedAssignee, msg.Assignee)
			}
		})
	}
}
