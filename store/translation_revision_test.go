package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestTranslationConditionalSaveRejectsOldSource(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	ctx := context.Background()
	result, err := GetDB().ExecContext(ctx, "INSERT INTO messages(user_email, task, source, source_ts) VALUES(?, 'A', 'slack', ?)", testutil.RandomEmail("conditional"), testutil.RandomTS("translation"))
	if err != nil {
		t.Fatal(err)
	}
	rawID, _ := result.LastInsertId()
	id := MessageID(rawID)
	if _, err := GetDB().ExecContext(ctx, "UPDATE messages SET task='B' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	accepted, err := SaveTaskTranslationsForSources(ctx, "ko", map[MessageID]string{id: "B translation"}, map[MessageID]string{id: "B"})
	if err != nil || accepted[id] != "B translation" {
		t.Fatalf("save B: %v, %v", accepted, err)
	}
	accepted, err = SaveTaskTranslationsForSources(ctx, "ko", map[MessageID]string{id: "late A translation"}, map[MessageID]string{id: "A"})
	if err != nil || len(accepted) != 0 {
		t.Fatalf("late A accepted: %v, %v", accepted, err)
	}
	cached, err := GetTaskTranslationsBatch(ctx, []MessageID{id}, "ko")
	if err != nil || cached[id] != "B translation" {
		t.Fatalf("B overwritten: %v, %v", cached, err)
	}
}
