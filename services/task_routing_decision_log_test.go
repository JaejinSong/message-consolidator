package services

import (
	"errors"
	"testing"

	"message-consolidator/store"
)

func newItemPtr(id int64) *store.MessageID {
	mid := store.MessageID(id)
	return &mid
}

// Why: task_id used to echo the AI's own item.ID, so a "new" decision always logged
// task_id:0 whether it produced a row or was dropped — the two were indistinguishable.
func TestDecisionEnvelope_TaskIDPrefersPersistedRow(t *testing.T) {
	item := store.TodoItem{State: "new", Task: "Pin down the AXWay agent name"}
	msg := store.ConsolidatedMessage{UserEmail: "u@x", Source: store.SourceSlack, Room: "biz-global-tech"}

	got := decisionEnvelope("u@x", item, msg, store.MessageID(13277), nil)

	if got.TaskID == nil || *got.TaskID != 13277 {
		t.Fatalf("TaskID = %v, want 13277", got.TaskID)
	}
	if got.State != "new" || got.Room != "biz-global-tech" {
		t.Errorf("envelope = %+v, want state=new room=biz-global-tech", got)
	}
}

func TestDecisionEnvelope_FallsBackToItemID(t *testing.T) {
	item := store.TodoItem{State: "update", Task: "t", ID: newItemPtr(11894)}
	msg := store.ConsolidatedMessage{UserEmail: "u@x", Source: store.SourceSlack}

	got := decisionEnvelope("u@x", item, msg, 0, nil)

	if got.TaskID == nil || *got.TaskID != 11894 {
		t.Fatalf("TaskID = %v, want 11894", got.TaskID)
	}
}

// Why: the five runbook tasks were logged as "new" and never persisted; the log has
// to say so or the loss stays invisible.
func TestDecisionEnvelope_MarksUnpersistedDecision(t *testing.T) {
	item := store.TodoItem{State: "new", Task: "Attach HTTP call collection", Reasoning: "explicit ask"}
	msg := store.ConsolidatedMessage{UserEmail: "u@x", Source: store.SourceSlack, SourceTS: "1789546445.856419"}

	got := decisionEnvelope("u@x", item, msg, 0, nil)

	if got.TaskID != nil {
		t.Errorf("TaskID = %v, want nil", got.TaskID)
	}
	if !containsAll(got.Reasoning, "explicit ask", "persisted=none") {
		t.Errorf("Reasoning = %q, want original reason plus persisted=none", got.Reasoning)
	}
}

func TestDecisionEnvelope_ErrorIsNotReportedAsDrop(t *testing.T) {
	item := store.TodoItem{State: "new", Task: "t"}
	msg := store.ConsolidatedMessage{UserEmail: "u@x", Source: store.SourceSlack}

	got := decisionEnvelope("u@x", item, msg, 0, errors.New("db locked"))

	if containsAll(got.Reasoning, "persisted=none") {
		t.Errorf("Reasoning = %q, want no persisted=none marker when the error path already reports it", got.Reasoning)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		found := false
		for i := 0; i+len(p) <= len(s); i++ {
			if s[i:i+len(p)] == p {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
