package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"message-consolidator/internal/testutil"
)

func TestUpdateTaskFullAppendTranslation(t *testing.T) {
	for _, mode := range []string{"nil", "db", "tx"} {
		for _, scenario := range []string{"changed", "unchanged", "empty", "missing", "cross-user", "cross-room", "delete-failure"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				checkFullAppendTranslation(t, mode, scenario)
			})
		}
	}
}

func checkFullAppendTranslation(t *testing.T, mode, scenario string) {
	t.Helper()
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup database: %v", err)
	}
	t.Cleanup(cleanup)
	ctx := context.Background()
	email, room := testutil.RandomEmail("append-translation"), "room-a"
	result, err := GetDB().ExecContext(ctx, "INSERT INTO messages (user_email, room, task, original_text, source, source_ts) VALUES (?, ?, ?, ?, ?, ?)", email, room, "Original title", "Original text", "slack", testutil.RandomTS("append"))
	if err != nil {
		t.Fatalf("seed message: %v", err)
	}
	rawID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("message id: %v", err)
	}
	id := MessageID(rawID)
	if err := SaveTaskTranslationsBulk(ctx, "ko", map[MessageID]string{id: "Original translation"}); err != nil {
		t.Fatalf("seed translation: %v", err)
	}
	if scenario == "delete-failure" {
		_, err = GetDB().ExecContext(ctx, "CREATE TRIGGER reject_append_translation_delete BEFORE DELETE ON task_translations BEGIN SELECT RAISE(ABORT, 'injected delete failure'); END")
		if err != nil {
			t.Fatalf("install trigger: %v", err)
		}
	}
	var q Querier
	var tx *sql.Tx
	switch mode {
	case "db":
		q = GetDB()
	case "tx":
		tx, err = GetDB().BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin transaction: %v", err)
		}
		defer tx.Rollback()
		q = tx
	}
	updateEmail, updateRoom, updateID, title := email, room, id, "Changed title 한글 🚀"
	switch scenario {
	case "unchanged":
		title = "Original title"
	case "empty":
		title = " "
	case "missing":
		updateID += 1000000
	case "cross-user":
		updateEmail = "other@example.com"
	case "cross-room":
		updateRoom = "other-room"
	}
	err = UpdateTaskFullAppend(ctx, q, updateEmail, updateRoom, updateID, title, "Added text")
	if scenario == "delete-failure" {
		if err == nil || !strings.Contains(err.Error(), "injected delete failure") {
			t.Errorf("expected injected delete failure, got %v", err)
		}
	} else if err != nil {
		t.Fatalf("update message: %v", err)
	}
	if tx != nil {
		if scenario == "delete-failure" {
			err = tx.Rollback()
		} else {
			err = tx.Commit()
		}
		if err != nil {
			t.Fatalf("finish caller transaction: %v", err)
		}
	}
	wantTitle, wantText, wantCount := "Original title", "Original text", 1
	switch scenario {
	case "changed":
		wantTitle, wantText, wantCount = title, "Added text\n\nOriginal text", 0
	case "unchanged", "empty":
		wantText = "Added text\n\nOriginal text"
	}
	var gotTitle, gotText string
	if err := GetDB().QueryRowContext(ctx, "SELECT task, original_text FROM messages WHERE id = ?", id).Scan(&gotTitle, &gotText); err != nil {
		t.Fatalf("read message: %v", err)
	}
	if gotTitle != wantTitle || gotText != wantText {
		t.Errorf("message = (%q, %q), want (%q, %q)", gotTitle, gotText, wantTitle, wantText)
	}
	var count int
	if err := GetDB().QueryRowContext(ctx, "SELECT COUNT(*) FROM task_translations WHERE message_id = ?", id).Scan(&count); err != nil {
		t.Fatalf("read translations: %v", err)
	}
	if count != wantCount {
		t.Errorf("translation count = %d, want %d", count, wantCount)
	}
	translations, err := GetTaskTranslationsBatch(ctx, []MessageID{id}, "ko")
	if err != nil {
		t.Fatalf("read cached translation: %v", err)
	}
	wantTranslation := "Original translation"
	if wantCount == 0 {
		wantTranslation = ""
	}
	if translations[id] != wantTranslation {
		t.Errorf("cached translation = %q, want %q", translations[id], wantTranslation)
	}
}
