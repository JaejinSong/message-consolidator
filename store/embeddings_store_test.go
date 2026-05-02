package store

import (
	"context"
	"testing"

	"message-consolidator/internal/testutil"
)

// TestEmbeddingsStoreLifecycle exercises Upsert → Get → ListMissing → ListPage →
// FTS top IDs against the real in-memory schema so we catch sqlc/migration drift
// the next time a query gets renamed.
func TestEmbeddingsStoreLifecycle(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("emb")

	insertArchived := func(taskText string) MessageID {
		t.Helper()
		res, err := GetDB().ExecContext(ctx,
			`INSERT INTO messages (user_email, task, source, source_ts, original_text, done, completed_at)
			 VALUES (?, ?, 'gmail', ?, ?, 1, datetime('now'))`,
			email, taskText, testutil.RandomTS(taskText), "body of "+taskText)
		if err != nil {
			t.Fatalf("insert msg: %v", err)
		}
		id, _ := res.LastInsertId()
		return MessageID(id)
	}

	id1 := insertArchived("incident triage runbook")
	id2 := insertArchived("quarterly pricing review")

	const model = "stub"

	missing, err := ListMissingEmbeddings(ctx, email, model, 10)
	if err != nil {
		t.Fatalf("ListMissingEmbeddings: %v", err)
	}
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing rows, got %d", len(missing))
	}

	count, err := CountMissingEmbeddings(ctx, email, model)
	if err != nil {
		t.Fatalf("CountMissingEmbeddings: %v", err)
	}
	if count != 2 {
		t.Errorf("count: want 2, got %d", count)
	}

	vec1 := []byte{0x00, 0x00, 0x80, 0x3f, 0x00, 0x00, 0x00, 0x00} // float32(1, 0)
	if err := UpsertEmbedding(ctx, id1, model, 2, vec1, "hash1"); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	gotVec, gotHash, gotDim, ok, err := GetEmbedding(ctx, id1)
	if err != nil || !ok {
		t.Fatalf("GetEmbedding ok=%v err=%v", ok, err)
	}
	if gotHash != "hash1" || gotDim != 2 || len(gotVec) != 8 {
		t.Errorf("get: hash=%q dim=%d vecLen=%d", gotHash, gotDim, len(gotVec))
	}

	if _, _, _, ok, err := GetEmbedding(ctx, id2); err != nil || ok {
		t.Errorf("expected ok=false for unembedded msg (ok=%v err=%v)", ok, err)
	}

	count, _ = CountMissingEmbeddings(ctx, email, model)
	if count != 1 {
		t.Errorf("after one upsert: want 1 missing, got %d", count)
	}

	page, err := ListArchiveEmbeddingsPage(ctx, email, model, 10, 0)
	if err != nil {
		t.Fatalf("ListArchiveEmbeddingsPage: %v", err)
	}
	if len(page) != 1 || page[0].MessageID != id1 {
		t.Errorf("page rows: %+v", page)
	}

	// FTS lookup hits messages_fts triggers; verify rowid comes back.
	ids, err := ArchiveFTSTopIDs(ctx, email, "incident", 10)
	if err != nil {
		t.Fatalf("ArchiveFTSTopIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != id1 {
		t.Errorf("fts ids: %v (id1=%d id2=%d)", ids, id1, id2)
	}

	// Empty/zero-limit guards.
	if got, _ := ArchiveFTSTopIDs(ctx, email, "", 10); got != nil {
		t.Errorf("empty query should return nil, got %v", got)
	}
	if got, _ := ArchiveFTSTopIDs(ctx, email, "incident", 0); got != nil {
		t.Errorf("zero limit should return nil, got %v", got)
	}

	// Model rotation wipes the row.
	if err := DeleteEmbeddingsByModel(ctx, model); err != nil {
		t.Fatalf("DeleteEmbeddingsByModel: %v", err)
	}
	if _, _, _, ok, _ := GetEmbedding(ctx, id1); ok {
		t.Errorf("expected row gone after DeleteEmbeddingsByModel")
	}
}
