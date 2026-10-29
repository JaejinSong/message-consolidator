package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func setupGenerationTest(t *testing.T, prefix string) (string, MessageID) {
	t.Helper()
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	t.Cleanup(cleanup)

	email := testutil.RandomEmail(prefix)
	_, id, err := SaveMessage(context.Background(), GetDB(), ConsolidatedMessage{
		UserEmail: email, Source: "test", SourceTS: "gen-1", Task: "Gen Task",
	})
	if err != nil {
		t.Fatalf("SaveMessage failed: %v", err)
	}
	return email, id
}

func setPublishHook(t *testing.T, fn func()) {
	t.Helper()
	refreshPublishHook = fn
	t.Cleanup(func() { refreshPublishHook = nil })
}

func cacheState(email string) (activeReady, archiveReady bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return cacheInitialized[email], archiveInitialized[email]
}

func TestRefreshCache_InvalidatedDuringRefresh_NotPublished(t *testing.T) {
	email, id := setupGenerationTest(t, "gen-active")
	ctx := context.Background()

	setPublishHook(t, func() {
		// Why: Simulates a write landing after the refresh query but before its publish.
		if err := MarkMessageDone(ctx, GetDB(), email, id, true); err != nil {
			t.Errorf("MarkMessageDone failed: %v", err)
		}
	})
	if err := RefreshCache(ctx, email); err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}
	if active, _ := cacheState(email); active {
		t.Fatalf("stale refresh result must not mark the active cache valid")
	}

	refreshPublishHook = nil
	msgs, err := GetMessages(ctx, email)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected fresh read to exclude the done message, got %+v", msgs)
	}
}

func TestRefreshCache_NoInvalidation_Publishes(t *testing.T) {
	email, _ := setupGenerationTest(t, "gen-plain")

	if err := RefreshCache(context.Background(), email); err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}
	if active, _ := cacheState(email); !active {
		t.Errorf("refresh without concurrent invalidation must publish")
	}
}

func TestRefreshArchiveCache_InvalidatedDuringRefresh_NotPublished(t *testing.T) {
	email, id := setupGenerationTest(t, "gen-archive")
	ctx := context.Background()

	setPublishHook(t, func() {
		if err := MarkMessageDone(ctx, GetDB(), email, id, true); err != nil {
			t.Errorf("MarkMessageDone failed: %v", err)
		}
	})
	if err := RefreshArchiveCache(ctx, email); err != nil {
		t.Fatalf("RefreshArchiveCache failed: %v", err)
	}
	if _, archive := cacheState(email); archive {
		t.Fatalf("stale refresh result must not mark the archive cache valid")
	}

	refreshPublishHook = nil
	archived, _, err := GetArchivedMessagesFiltered(ctx, ArchiveFilter{Email: email, Limit: 50})
	if err != nil {
		t.Fatalf("GetArchivedMessagesFiltered failed: %v", err)
	}
	if len(archived) != 1 || !archived[0].Done {
		t.Errorf("expected fresh read to include the done message, got %+v", archived)
	}
}

func TestRefreshArchiveCache_NoInvalidation_Publishes(t *testing.T) {
	email, _ := setupGenerationTest(t, "gen-archive-plain")

	if err := RefreshArchiveCache(context.Background(), email); err != nil {
		t.Fatalf("RefreshArchiveCache failed: %v", err)
	}
	if _, archive := cacheState(email); !archive {
		t.Errorf("archive refresh without concurrent invalidation must publish")
	}
}

func TestRefresh_InvalidatingOtherUser_DoesNotBlockPublish(t *testing.T) {
	emailB, _ := setupGenerationTest(t, "gen-user-b")
	emailA := testutil.RandomEmail("gen-user-a")

	setPublishHook(t, func() { InvalidateCache(emailA) })
	ctx := context.Background()
	if err := RefreshCache(ctx, emailB); err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}
	if err := RefreshArchiveCache(ctx, emailB); err != nil {
		t.Fatalf("RefreshArchiveCache failed: %v", err)
	}
	active, archive := cacheState(emailB)
	if !active || !archive {
		t.Errorf("invalidating another user must not block publish: active=%v archive=%v", active, archive)
	}
}
