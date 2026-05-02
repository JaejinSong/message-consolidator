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

func TestSplitTranslationsByCache_NoCache(t *testing.T) {
	t.Parallel()
	translationMu.Lock()
	delete(translationCache, "xx")
	translationMu.Unlock()

	ids := []MessageID{1, 2, 3}
	results, missing := splitTranslationsByCache("xx", ids)
	if len(results) != 0 {
		t.Errorf("expected 0 cached results, got %d", len(results))
	}
	if len(missing) != 3 {
		t.Errorf("expected 3 missing, got %d", len(missing))
	}
}

func TestSplitTranslationsByCache_PartialHit(t *testing.T) {
	t.Parallel()
	lang := "test-lang-partial"
	translationMu.Lock()
	translationCache[lang] = map[MessageID]string{
		MessageID(10): "hello",
		MessageID(11): "",
	}
	translationMu.Unlock()
	t.Cleanup(func() {
		translationMu.Lock()
		delete(translationCache, lang)
		translationMu.Unlock()
	})

	ids := []MessageID{10, 11, 12}
	results, missing := splitTranslationsByCache(lang, ids)
	// ID 10: cached with text → in results
	if results[10] != "hello" {
		t.Errorf("expected 'hello' for ID 10, got %q", results[10])
	}
	// ID 11: cached with empty → not in results but not in missing
	// ID 12: not cached → in missing
	for _, m := range missing {
		if m != 12 {
			t.Errorf("unexpected missing id %d", m)
		}
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
