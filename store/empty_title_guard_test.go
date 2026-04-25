package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"strings"
	"testing"
)

// Regression: empty/whitespace newTitle to MergeTasksWithTitle must not wipe
// the destination row's task field. Reproduces the silent-loss path that left
// row 11657 with task="" and hidden from the active dashboard list.
func TestMergeTasksWithTitle_EmptyTitle_PreservesDest(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("merge-empty")
	room := "test-room"

	_, destID, err := SaveMessage(ctx, GetDB(), ConsolidatedMessage{
		UserEmail: email, Source: "slack", Room: room, SourceTS: "dest-1",
		Task: "Existing Title", OriginalText: "dest body",
	})
	if err != nil {
		t.Fatalf("save dest: %v", err)
	}
	_, srcID, err := SaveMessage(ctx, GetDB(), ConsolidatedMessage{
		UserEmail: email, Source: "slack", Room: room, SourceTS: "src-1",
		Task: "Source Title", OriginalText: "src body",
	})
	if err != nil {
		t.Fatalf("save src: %v", err)
	}

	cases := []struct {
		name  string
		title string
	}{
		{"empty string", ""},
		{"spaces only", "   "},
		{"newline only", "\n"},
		{"tabs only", "\t\t"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := MergeTasksWithTitle(ctx, email, []MessageID{srcID}, destID, tc.title)
			if err == nil {
				// Even if the call succeeds (because guard substituted dest.Task),
				// dest must not have an empty task.
				m, gErr := GetMessageByID(ctx, GetDB(), email, destID)
				if gErr != nil {
					t.Fatalf("re-read dest: %v", gErr)
				}
				if strings.TrimSpace(m.Task) == "" {
					t.Errorf("dest.task collapsed to whitespace despite guard: %q", m.Task)
				}
			}
			// If error: guard rejected the merge entirely. Verify dest still readable.
			m, gErr := GetMessageByID(ctx, GetDB(), email, destID)
			if gErr != nil {
				t.Fatalf("post-error re-read: %v", gErr)
			}
			if strings.TrimSpace(m.Task) == "" {
				t.Errorf("dest.task became empty after rejected merge: %q", m.Task)
			}
		})
	}
}

// Regression: UpdateTaskText with blank input must not wipe an existing title.
func TestUpdateTaskText_EmptyInput_NoOp(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("upd-empty")

	_, id, err := SaveMessage(ctx, GetDB(), ConsolidatedMessage{
		UserEmail: email, Source: "slack", SourceTS: "u-1", Task: "Keep Me",
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	for _, blank := range []string{"", "   ", "\n\t"} {
		if err := UpdateTaskText(ctx, GetDB(), email, id, blank); err != nil {
			t.Fatalf("UpdateTaskText(%q) returned error: %v", blank, err)
		}
		m, err := GetMessageByID(ctx, GetDB(), email, id)
		if err != nil {
			t.Fatalf("re-read: %v", err)
		}
		if m.Task != "Keep Me" {
			t.Errorf("blank=%q: task changed to %q", blank, m.Task)
		}
	}
}

// Regression: MarkMessageDone(false) must clear completed_at, not leave the
// timestamp from a prior completion. Reproduces the row-11657 anomaly where
// completed_at remained populated even though done was flipped back to 0.
func TestMarkMessageDone_Undo_ClearsCompletedAt(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("undo")

	_, id, err := SaveMessage(ctx, GetDB(), ConsolidatedMessage{
		UserEmail: email, Source: "slack", SourceTS: "u-1", Task: "T",
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := MarkMessageDone(ctx, GetDB(), email, id, true); err != nil {
		t.Fatalf("mark done: %v", err)
	}
	mDone, _ := GetMessageByID(ctx, GetDB(), email, id)
	if mDone.CompletedAt == nil {
		t.Fatalf("post-done: completed_at must be populated")
	}

	if err := MarkMessageDone(ctx, GetDB(), email, id, false); err != nil {
		t.Fatalf("undo done: %v", err)
	}
	mUndone, _ := GetMessageByID(ctx, GetDB(), email, id)
	if mUndone.Done {
		t.Errorf("done should be false after undo")
	}
	if mUndone.CompletedAt != nil {
		t.Errorf("completed_at must be NULL after undo, got %v", *mUndone.CompletedAt)
	}
}

// Regression: UpdateTaskFullAppend with blank newTask must preserve the title
// while still appending to original_text (audit trail intact).
func TestUpdateTaskFullAppend_EmptyTask_AppendsOnly(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("upd-append")
	room := "test-room"

	_, id, err := SaveMessage(ctx, GetDB(), ConsolidatedMessage{
		UserEmail: email, Source: "slack", Room: room, SourceTS: "ua-1",
		Task: "Original Title", OriginalText: "first",
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := UpdateTaskFullAppend(ctx, GetDB(), email, room, id, "", "appended-chunk"); err != nil {
		t.Fatalf("UpdateTaskFullAppend: %v", err)
	}

	m, err := GetMessageByID(ctx, GetDB(), email, id)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if m.Task != "Original Title" {
		t.Errorf("task overwritten by empty input: got %q", m.Task)
	}
	if !strings.Contains(m.OriginalText, "appended-chunk") {
		t.Errorf("original_text did not receive append: %q", m.OriginalText)
	}
}
