package scanner

import (
	"testing"

	"message-consolidator/store"
)

func newItem(state, sourceTS, task, assignee string) store.TodoItem {
	return store.TodoItem{State: state, SourceTS: sourceTS, Task: task, Assignee: assignee}
}

// Why: one message carries one source_ts, and UNIQUE(user_email, source_ts) lets it own
// one row — the 2nd..Nth new item from the same message were dropped by ON CONFLICT
// with no error. Fold them into the surviving item instead of losing them.
func TestFoldSameMessageNewItems_FoldsIntoFirst(t *testing.T) {
	items := []store.TodoItem{
		newItem("new", "1789546445.856419", "Pin down the AXWay agent name", "Yoga Wiranda"),
		newItem("new", "1789546445.856419", "Fill in the missing URL names", "Yoga Wiranda"),
		newItem("new", "1789546445.856419", "Attach HTTP call collection", "Hady"),
	}

	got := foldSameMessageNewItems(items)

	if len(got) != 1 {
		t.Fatalf("items = %d, want 1", len(got))
	}
	if got[0].Task != "Pin down the AXWay agent name" {
		t.Errorf("surviving task = %q, want the first item", got[0].Task)
	}
	if len(got[0].Subtasks) != 2 {
		t.Fatalf("subtasks = %d, want 2", len(got[0].Subtasks))
	}
	if got[0].Subtasks[0].Task != "Fill in the missing URL names" || got[0].Subtasks[0].AssigneeName != "Yoga Wiranda" {
		t.Errorf("subtask[0] = %+v, want the 2nd ask with its assignee", got[0].Subtasks[0])
	}
	if got[0].Subtasks[1].AssigneeName != "Hady" {
		t.Errorf("subtask[1] assignee = %q, want Hady", got[0].Subtasks[1].AssigneeName)
	}
}

func TestFoldSameMessageNewItems_KeepsDistinctMessagesApart(t *testing.T) {
	items := []store.TodoItem{
		newItem("new", "1789546445.856419", "Task A", ""),
		newItem("new", "1789524347.787899", "Task B", ""),
	}

	got := foldSameMessageNewItems(items)

	if len(got) != 2 {
		t.Fatalf("items = %d, want 2 — different messages must stay separate", len(got))
	}
	if len(got[0].Subtasks) != 0 || len(got[1].Subtasks) != 0 {
		t.Errorf("distinct messages must not gain subtasks: %+v", got)
	}
}

// Why: update/resolve target an existing row by ID and never contend for the insert,
// so folding them would silently discard a state transition.
func TestFoldSameMessageNewItems_LeavesNonNewStatesAlone(t *testing.T) {
	existing := store.MessageID(11894)
	items := []store.TodoItem{
		newItem("new", "1789546445.856419", "Pin down the AXWay agent name", ""),
		{State: "update", SourceTS: "1789546445.856419", Task: "Resolve Axway naming", ID: &existing},
		{State: "resolve", SourceTS: "1789546445.856419", Task: "", ID: &existing},
	}

	got := foldSameMessageNewItems(items)

	if len(got) != 3 {
		t.Fatalf("items = %d, want 3", len(got))
	}
	if len(got[0].Subtasks) != 0 {
		t.Errorf("new item gained subtasks from non-new states: %+v", got[0].Subtasks)
	}
}

func TestFoldSameMessageNewItems_PreservesItemOwnSubtasksAndSkipsDuplicates(t *testing.T) {
	first := newItem("new", "ts1", "Umbrella", "")
	first.Subtasks = []store.TodoSubtask{{Task: "Existing subtask"}}
	items := []store.TodoItem{
		first,
		newItem("new", "ts1", "Existing subtask", ""),
		newItem("new", "ts1", "Umbrella", ""),
		newItem("new", "ts1", "Genuinely new ask", ""),
	}

	got := foldSameMessageNewItems(items)

	if len(got) != 1 {
		t.Fatalf("items = %d, want 1", len(got))
	}
	if len(got[0].Subtasks) != 2 {
		t.Fatalf("subtasks = %+v, want the original plus one new ask", got[0].Subtasks)
	}
	if got[0].Subtasks[1].Task != "Genuinely new ask" {
		t.Errorf("subtask[1] = %q, want %q", got[0].Subtasks[1].Task, "Genuinely new ask")
	}
}

func TestFoldSameMessageNewItems_IgnoresEmptySourceTS(t *testing.T) {
	items := []store.TodoItem{
		newItem("new", "", "Task A", ""),
		newItem("new", "", "Task B", ""),
	}

	if got := foldSameMessageNewItems(items); len(got) != 2 {
		t.Errorf("items = %d, want 2 — an empty source_ts is not an identity", len(got))
	}
}
