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
		// Body-text group mentions no longer override category; rely on AI to set Assignee="shared".
		{"Empty assignee + @channel body → others", "", "", "@channel update please", CategoryOthers, ""},
		{"Empty assignee + @here body → others", "", "", "@here check this", CategoryOthers, ""},
	}

	// Requester 정규화: RequesterCanonical이 user.Email이면 표시명을 PreferredName으로 통일
	t.Run("Requester normalization: Korean requester name normalized to PreferredName", func(t *testing.T) {
		msg := &store.ConsolidatedMessage{
			Assignee:           "Other Person",
			Requester:          "송재진",
			RequesterCanonical: email,
			Task:               "some task",
		}
		service.applyAssigneeRules(user, identities, msg)

		if msg.Requester != name {
			t.Errorf("expected requester %q, got %q", name, msg.Requester)
		}
		if msg.RequesterCanonical != email {
			t.Errorf("expected requester_canonical %q, got %q", email, msg.RequesterCanonical)
		}
	})

	// AssigneeCanonical fallback: 표시명이 identities와 매칭 안 되더라도 canonical이 user.Email이면 personal
	t.Run("AssigneeCanonical fallback: Korean display name not in identities", func(t *testing.T) {
		msg := &store.ConsolidatedMessage{
			Assignee:          "'송재진 (JJ Song)'",
			Requester:         "'송재진 (JJ Song)'",
			RequesterCanonical: email,
			AssigneeCanonical: email,
			Task:              "some task",
		}
		service.applyAssigneeRules(user, identities, msg)
		service.assignCategory(user, identities, msg)

		if msg.Category != CategoryPersonal {
			t.Errorf("AssigneeCanonical fallback: expected %s, got %s", CategoryPersonal, msg.Category)
		}
		if msg.Assignee != name {
			t.Errorf("AssigneeCanonical fallback: expected assignee %q, got %q", name, msg.Assignee)
		}
	})

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
