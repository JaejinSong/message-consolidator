package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestGetTaskTranslationsBatch_Empty(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	result, err := GetTaskTranslationsBatch(context.Background(), nil, "ko")
	if err != nil {
		t.Fatalf("GetTaskTranslationsBatch empty: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}
}

func TestGetTaskTranslationsBatch_EmptyLang(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	// Empty lang defaults to "en", no rows exist → empty map.
	result, err := GetTaskTranslationsBatch(context.Background(), []MessageID{1, 2}, "")
	if err != nil {
		t.Fatalf("GetTaskTranslationsBatch emptyLang: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil map")
	}
}

func TestGetTaskTranslationsBatch_MissingTranslations(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup database: %v", err)
	}
	t.Cleanup(cleanup)
	results, err := GetTaskTranslationsBatch(context.Background(), []MessageID{1, 2, 3}, "xx")
	if err != nil {
		t.Fatalf("get missing translations: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no translations, got %v", results)
	}
}

func TestGetTaskTranslationsBatch_ReflectsPersistedChanges(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup database: %v", err)
	}
	t.Cleanup(cleanup)
	ctx := context.Background()
	result, err := GetDB().ExecContext(ctx, "INSERT INTO messages (user_email, task, source, source_ts) VALUES (?, ?, ?, ?)", testutil.RandomEmail("translation"), "Title", "slack", testutil.RandomTS("translation"))
	if err != nil {
		t.Fatalf("seed message: %v", err)
	}
	rawID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("message id: %v", err)
	}
	id := MessageID(rawID)
	for _, text := range []string{"First translation", "Updated translation"} {
		if err := SaveTaskTranslationsBulk(ctx, "ko", map[MessageID]string{id: text}); err != nil {
			t.Fatalf("save translation: %v", err)
		}
		results, err := GetTaskTranslationsBatch(ctx, []MessageID{id, id + 1000000}, "ko")
		if err != nil {
			t.Fatalf("get translations: %v", err)
		}
		if len(results) != 1 || results[id] != text {
			t.Fatalf("translations = %v, want only %q", results, text)
		}
	}
	InvalidateTaskTranslation(ctx, nil, id)
	results, err := GetTaskTranslationsBatch(ctx, []MessageID{id}, "ko")
	if err != nil || len(results) != 0 {
		t.Fatalf("translations after invalidation = %v, %v", results, err)
	}
}

func TestSaveTaskTranslationsBulk_Empty(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	// Empty results map should be a no-op.
	if err := SaveTaskTranslationsBulk(context.Background(), "en", map[MessageID]string{}); err != nil {
		t.Errorf("SaveTaskTranslationsBulk empty: %v", err)
	}
}

func TestGetGmailToken_Unknown(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	_, err = GetGmailToken(context.Background(), "nobody@example.com")
	if err == nil {
		t.Error("expected error for unknown email, got nil")
	}
}

func TestIncrementFilteredCount(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	// No existing usage row — should not panic.
	IncrementFilteredCount("u@example.com")
}
