package store

import (
	"strings"
	"testing"
)

func makeTodo(task, state, groupID string, score int) TodoItem {
	return TodoItem{
		Task:            task,
		State:           state,
		AffinityGroupID: groupID,
		AffinityScore:   score,
		AssignedAt:      "2026-04-24T00:00:00Z",
	}
}

func TestConsolidateMergeInto_DuplicateContent(t *testing.T) {
	same := "Raise the request to the dev team once business context is provided."
	primary := makeTodo(same, "new", "g1", 90)
	secondary := makeTodo(same, "update", "g1", 90)

	result := consolidateMergeInto(primary, secondary)

	if strings.Contains(result.Task, "--- [Update:") {
		t.Errorf("duplicate content should not produce an update block, got: %s", result.Task)
	}
	if result.Task != same {
		t.Errorf("task should be unchanged, got: %s", result.Task)
	}
}

func TestConsolidateMergeInto_NewContent(t *testing.T) {
	primary := makeTodo("Original task.", "new", "g1", 90)
	secondary := makeTodo("Additional update info.", "update", "g1", 90)

	result := consolidateMergeInto(primary, secondary)

	if !strings.Contains(result.Task, "--- [Update: 2026-04-24] ---") {
		t.Errorf("expected update block, got: %s", result.Task)
	}
	if !strings.Contains(result.Task, "Additional update info.") {
		t.Errorf("expected secondary content, got: %s", result.Task)
	}
}

func TestConsolidateMergeInto_ContentAlreadyIncluded(t *testing.T) {
	primary := makeTodo("Original task.\n\n--- [Update: 2026-04-23] ---\nPrev update.", "new", "g1", 90)
	secondary := makeTodo("Prev update.", "update", "g1", 90)

	result := consolidateMergeInto(primary, secondary)

	count := strings.Count(result.Task, "Prev update.")
	if count != 1 {
		t.Errorf("already-included content should not be appended again, got count=%d in: %s", count, result.Task)
	}
}

func TestConsolidateTasks_NoDuplicateTitle(t *testing.T) {
	content := "Raise the request to the dev team."
	tasks := []TodoItem{
		makeTodo(content, "new", "g1", 90),
		makeTodo(content, "update", "g1", 90),
	}

	result := ConsolidateTasks(tasks)

	if len(result) != 1 {
		t.Fatalf("expected 1 task after consolidation, got %d", len(result))
	}
	if strings.Contains(result[0].Task, "--- [Update:") {
		t.Errorf("identical update should not append, got: %s", result[0].Task)
	}
}
