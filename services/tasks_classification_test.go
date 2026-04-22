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

	aliases, _ := store.GetUserAliasesByEmail(ctx, email)
	identities := GetEffectiveAliases(*user, aliases)

	tests := []struct {
		name             string
		assignee         string
		requester        string
		task             string
		expectedCategory string
		expectedAssignee string
	}{
		{"Exact Name Match", "Jaejin Song", "", "some task", CategoryPersonal, name},
		{"Alias Match (Korean)", "송재진", "", "some task", CategoryPersonal, name},
		{"Alias Match (Short)", "jj", "", "some task", CategoryPersonal, name},
		{"Token Match", "__CURRENT_USER__", "", "some task", CategoryPersonal, name},
		{"Literal 'me'", "me", "", "some task", CategoryPersonal, name},
		{"Non-match → others", "Other Person", "Other Person", "some task", CategoryOthers, "Other Person"},
		{"Undefined assignee → others", "undefined", "", "some task", CategoryOthers, ""},
		{"Unknown assignee → others", "unknown", "", "some task", CategoryOthers, ""},
		{"Requested: requester by email", "Other Person", email, "some task", CategoryRequested, "Other Person"},
		{"Requested: requester by name", "Other Person", name, "some task", CategoryRequested, "Other Person"},
		{"Shared: explicit shared assignee", "shared", "", "some task", CategoryShared, "shared"},
		{"Shared: @channel group mention", "", "", "@channel update please", CategoryShared, ""},
		{"Shared: @here group mention", "", "", "@here check this", CategoryShared, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &store.ConsolidatedMessage{
				Assignee:  tt.assignee,
				Requester: tt.requester,
				Task:      tt.task,
			}
			service.applyAssigneeRules(user, identities, msg)
			service.assignCategory(user, identities, msg)

			if msg.Category != tt.expectedCategory {
				t.Errorf("expected category %s, got %s", tt.expectedCategory, msg.Category)
			}
			if msg.Assignee != tt.expectedAssignee {
				t.Errorf("expected assignee %s, got %s", tt.expectedAssignee, msg.Assignee)
			}
		})
	}
}
