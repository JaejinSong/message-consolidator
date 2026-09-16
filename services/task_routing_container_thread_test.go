package services

import (
	"context"
	"testing"

	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func TestMergeThreadSubtasks_AppendsNewAsk(t *testing.T) {
	parent := store.ConsolidatedMessage{
		Task:     "Check what additional transaction information can be captured",
		Subtasks: []store.Subtask{{Task: "Pin down the AXWay agent name"}},
	}

	got := mergeThreadSubtasks(parent, nil, "Attach HTTP call collection", "Yoga Wiranda")

	if len(got) != 2 {
		t.Fatalf("subtasks = %d, want 2", len(got))
	}
	if got[1].Task != "Attach HTTP call collection" || got[1].Assignee != "Yoga Wiranda" {
		t.Errorf("appended subtask = %+v, want the new ask with its assignee", got[1])
	}
	if got[0].Task != "Pin down the AXWay agent name" {
		t.Errorf("existing subtask was disturbed: %+v", got[0])
	}
}

// Why: the sweeper re-reads a thread every hour; an unchanged reply must not pile up
// duplicate subtasks.
func TestMergeThreadSubtasks_SkipsTextAlreadyPresent(t *testing.T) {
	parent := store.ConsolidatedMessage{
		Task:     "Run collect-collzfs.sh for Smartfren",
		Subtasks: []store.Subtask{{Task: "Attach HTTP call collection"}},
	}

	if got := mergeThreadSubtasks(parent, nil, "attach HTTP call COLLECTION", ""); len(got) != 1 {
		t.Errorf("duplicate subtask appended: %+v", got)
	}
	if got := mergeThreadSubtasks(parent, nil, "Run collect-collzfs.sh for Smartfren", ""); len(got) != 1 {
		t.Errorf("subtask restating the parent title appended: %+v", got)
	}
}

func TestMergeThreadSubtasks_CarriesItemOwnSubtasks(t *testing.T) {
	parent := store.ConsolidatedMessage{Task: "Axway transaction naming"}
	incoming := []store.Subtask{{Task: "Fill in the missing URL names", Assignee: "Yoga Wiranda"}}

	got := mergeThreadSubtasks(parent, incoming, "Attach HTTP call collection", "Yoga Wiranda")

	if len(got) != 2 {
		t.Fatalf("subtasks = %d, want 2 (item subtask + the ask itself)", len(got))
	}
	if got[0].Task != "Fill in the missing URL names" {
		t.Errorf("incoming subtask missing: %+v", got)
	}
}

// Why: five runbook asks in one Slack thread renamed task 13277 five times and left no
// row for the other four. A container thread keeps its parent's identity.
func TestUpdateThreadParent_SlackReplyKeepsParentTitle(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := "container-thread@example.com"
	room := "biz-global-tech"
	const parentTitle = "Check what additional transaction information can be captured"
	parentID := seedSlackThreadParent(t, email, room, "1789461410.169779", parentTitle)

	// Why: the scanner folds the other asks from the same message into item.Subtasks
	// (foldSameMessageNewItems), so the routing must carry them through to the parent.
	item := store.TodoItem{
		State: "new",
		Task:  "Decide whether to keep the AXWay agent on the Node Manager",
		Subtasks: []store.TodoSubtask{
			{Task: "Fill in the missing URL names", AssigneeName: "Yoga Wiranda"},
		},
	}
	msg := store.ConsolidatedMessage{
		UserEmail: email, Source: store.SourceSlack, Room: room,
		SourceTS: "1789546445.856419", ThreadID: "1789461410.169779",
		OriginalText: "runbook, four tasks in priority order",
		Assignee:     "Yoga Wiranda",
	}

	gotID, err := HandleTaskState(context.Background(), nil, email, item, msg)
	if err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}
	if gotID != parentID {
		t.Fatalf("returned id = %d, want parent %d", gotID, parentID)
	}

	var task, subtasks string
	if err := store.GetDB().QueryRow("SELECT task, COALESCE(subtasks,'[]') FROM messages WHERE id = ?", parentID).Scan(&task, &subtasks); err != nil {
		t.Fatalf("read parent: %v", err)
	}
	if task != parentTitle {
		t.Errorf("parent task = %q, want unchanged %q", task, parentTitle)
	}
	if !containsAll(subtasks, "Decide whether to keep the AXWay agent", "Fill in the missing URL names") {
		t.Errorf("parent subtasks = %s, want the ask and the folded subtask attached", subtasks)
	}
}

func seedSlackThreadParent(t *testing.T, email, room, threadID, task string) store.MessageID {
	t.Helper()
	res, err := store.GetDB().Exec(
		"INSERT INTO messages (user_email, source, room, task, original_text, source_ts, thread_id, done, is_deleted) VALUES (?, 'slack', ?, ?, 'orig', ?, ?, 0, 0)",
		email, room, task, threadID, threadID,
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id64, _ := res.LastInsertId()
	return store.MessageID(id64)
}
