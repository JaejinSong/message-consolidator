package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"message-consolidator/config"
	"message-consolidator/internal/testutil"
)

func TestTitleUpdatesInvalidateTranslations(t *testing.T) {
	for _, path := range []string{"text", "correction"} {
		for _, mode := range []string{"nil", "db", "tx"} {
			for _, scenario := range []string{"changed", "unchanged", "non-title", "cross-user", "missing", "update-failure", "delete-failure"} {
				t.Run(path+"/"+mode+"/"+scenario, func(t *testing.T) {
					checkTitleTranslationUpdate(t, path, mode, scenario)
				})
			}
		}
	}
}

func TestTitleUpdateDoesNotRetainTranslationReadBeforeCommit(t *testing.T) {
	ctx := context.Background()
	testPath := "file:" + t.TempDir() + "/translations.db"
	cleanup, err := testutil.SetupTestDB(func(ctx context.Context, cfg *config.Config) error {
		TestDSN = testPath
		return InitDB(ctx, cfg)
	}, ResetForTest)
	if err != nil {
		t.Fatalf("setup database: %v", err)
	}
	t.Cleanup(cleanup)
	GetDB().SetMaxOpenConns(2)
	GetDB().SetMaxIdleConns(2)
	if _, err := GetDB().ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		t.Fatalf("enable WAL: %v", err)
	}
	email := testutil.RandomEmail("translation-race")
	result, err := GetDB().ExecContext(ctx, "INSERT INTO messages (user_email, task, source, source_ts) VALUES (?, ?, ?, ?)", email, "Original title", "slack", testutil.RandomTS("race"))
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
	tx, err := GetDB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback()
	if err := UpdateTaskText(ctx, tx, email, id, "Changed title"); err != nil {
		t.Fatalf("update title: %v", err)
	}
	translations, err := GetTaskTranslationsBatch(ctx, []MessageID{id}, "ko")
	if err != nil || translations[id] != "Original translation" {
		t.Fatalf("read committed translation during pending update: %v, %v", translations, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit update: %v", err)
	}
	translations, err = GetTaskTranslationsBatch(ctx, []MessageID{id}, "ko")
	if err != nil {
		t.Fatalf("read translations after commit: %v", err)
	}
	if translations[id] != "" {
		t.Fatalf("stale translation survived commit: %q", translations[id])
	}
}

func checkTitleTranslationUpdate(t *testing.T, path, mode, scenario string) {
	t.Helper()
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup database: %v", err)
	}
	t.Cleanup(cleanup)
	ctx := context.Background()
	email := testutil.RandomEmail("title-translation")
	result, err := GetDB().ExecContext(ctx, "INSERT INTO messages (user_email, task, source, source_ts) VALUES (?, ?, ?, ?)", email, "Original title", "slack", testutil.RandomTS("title"))
	if err != nil {
		t.Fatalf("seed message: %v", err)
	}
	rawID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("message id: %v", err)
	}
	id := MessageID(rawID)
	for _, lang := range []string{"ko", "en"} {
		if err := SaveTaskTranslationsBulk(ctx, lang, map[MessageID]string{id: "Original translation"}); err != nil {
			t.Fatalf("seed translation: %v", err)
		}
	}
	if scenario == "update-failure" || scenario == "delete-failure" {
		query := "CREATE TRIGGER reject_title_update BEFORE UPDATE ON messages BEGIN SELECT RAISE(ABORT, 'injected update failure'); END"
		if scenario == "delete-failure" {
			query = "CREATE TRIGGER reject_translation_delete BEFORE DELETE ON task_translations BEGIN SELECT RAISE(ABORT, 'injected delete failure'); END"
		}
		if _, err := GetDB().ExecContext(ctx, query); err != nil {
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
	updateEmail, updateID, title := email, id, "Changed title 한글 🚀"
	switch scenario {
	case "unchanged":
		title = "Original title"
	case "cross-user":
		updateEmail = "other@example.com"
	case "missing":
		updateID = id + 1000000
	}
	if path == "correction" {
		fields := UpdateMessageCorrectionFields{Task: nullString(title)}
		if scenario == "non-title" {
			fields.Task = sql.NullString{}
			fields.Assignee = nullString("Updated assignee")
		}
		err = UpdateMessageCorrection(ctx, q, updateEmail, updateID, fields)
	} else if scenario == "non-title" {
		err = UpdateTaskAssignee(ctx, q, updateEmail, updateID, "Updated assignee")
	} else {
		err = UpdateTaskText(ctx, q, updateEmail, updateID, title)
	}
	wantError := scenario == "update-failure" || scenario == "delete-failure"
	if wantError {
		if err == nil || !strings.Contains(err.Error(), "injected") {
			t.Errorf("expected injected error, got %v", err)
		}
	} else if err != nil {
		t.Fatalf("update message: %v", err)
	}
	if tx != nil {
		if wantError {
			if err := tx.Rollback(); err != nil {
				t.Fatalf("rollback caller transaction: %v", err)
			}
		} else if err := tx.Commit(); err != nil {
			t.Fatalf("commit caller transaction: %v", err)
		}
	}
	wantTitle, wantCount := "Original title", 2
	if scenario == "changed" {
		wantTitle, wantCount = title, 0
	}
	var gotTitle string
	if err := GetDB().QueryRowContext(ctx, "SELECT task FROM messages WHERE id = ?", id).Scan(&gotTitle); err != nil {
		t.Fatalf("read title: %v", err)
	}
	if gotTitle != wantTitle {
		t.Errorf("title = %q, want %q", gotTitle, wantTitle)
	}
	var count int
	if err := GetDB().QueryRowContext(ctx, "SELECT COUNT(*) FROM task_translations WHERE message_id = ?", id).Scan(&count); err != nil {
		t.Fatalf("read translations: %v", err)
	}
	if count != wantCount {
		t.Errorf("translation count = %d, want %d", count, wantCount)
	}
	for _, lang := range []string{"ko", "en"} {
		translations, err := GetTaskTranslationsBatch(ctx, []MessageID{id}, lang)
		if err != nil {
			t.Fatalf("read cached translations: %v", err)
		}
		want := "Original translation"
		if scenario == "changed" {
			want = ""
		}
		if translations[id] != want {
			t.Errorf("cached %s translation = %q, want %q", lang, translations[id], want)
		}
	}
}
