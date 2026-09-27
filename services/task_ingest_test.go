package services

import (
	"message-consolidator/store"
	"reflect"
	"testing"
)

func TestGuardAndBuild_KeptItemReturnsBuiltMessage(t *testing.T) {
	if store.GetDB() != nil {
		t.Skip("skipping: DB connection present, test assumes nil DB path")
	}
	p := TaskBuildParams{
		UserEmail:    "user@example.com",
		OriginalText: "Please send the report by Friday",
		Item: store.TodoItem{
			Task:     "Send report",
			Category: "TASK",
		},
	}
	item, msg, guard, ok := GuardAndBuild(t.Context(), p)
	if !ok {
		t.Fatalf("GuardAndBuild() ok = false; want true (DropReason=%q)", guard.DropReason)
	}
	if item.Task != "Send report" {
		t.Errorf("item.Task = %q; want %q", item.Task, "Send report")
	}
	if msg.UserEmail != "user@example.com" {
		t.Errorf("msg.UserEmail = %q; want %q", msg.UserEmail, "user@example.com")
	}
}

func TestGuardAndBuild_DroppedItemReturnsDropReason(t *testing.T) {
	if store.GetDB() != nil {
		t.Skip("skipping: DB connection present, test assumes nil DB path")
	}
	p := TaskBuildParams{
		UserEmail:    "user@example.com",
		OriginalText: "Completely unrelated chit chat about the weather",
		Item: store.TodoItem{
			Task:     "Grant server access",
			Category: "TASK",
		},
	}
	item, msg, guard, ok := GuardAndBuild(t.Context(), p)
	if ok {
		t.Fatalf("GuardAndBuild() ok = true; want false")
	}
	if guard.DropReason != "no_token_overlap" {
		t.Errorf("guard.DropReason = %q; want %q", guard.DropReason, "no_token_overlap")
	}
	if !reflect.DeepEqual(item, store.TodoItem{}) {
		t.Errorf("item = %+v; want zero value", item)
	}
	if !reflect.DeepEqual(msg, store.ConsolidatedMessage{}) {
		t.Errorf("msg = %+v; want zero value", msg)
	}
}
