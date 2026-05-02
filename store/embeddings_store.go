package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"message-consolidator/db"
)

// MessageEmbeddingRow mirrors the (message_id, vec) projection used by the
// hybrid ranker. Why: sqlc's row type lives in the db package; re-exporting
// here lets services consume a flat shape without a db import.
type MessageEmbeddingRow struct {
	MessageID MessageID
	Vec       []byte
}

// MissingEmbeddingRow describes an archive message that needs embedding work.
type MissingEmbeddingRow struct {
	ID           MessageID
	Task         string
	OriginalText string
}

// UpsertEmbedding writes (or replaces) a single message's embedding bytes.
// dim is stored alongside the blob so future model swaps can detect mismatched
// shapes without decoding the BLOB.
func UpsertEmbedding(ctx context.Context, msgID MessageID, model string, dim int, vec []byte, textHash string) error {
	q := db.New(GetDB())
	return q.UpsertMessageEmbedding(ctx, db.UpsertMessageEmbeddingParams{
		MessageID: int64(msgID),
		Model:     model,
		Dim:       int64(dim),
		Vec:       vec,
		TextHash:  textHash,
	})
}

// GetEmbedding returns (vec, hash, dim, ok). ok=false when no row exists.
func GetEmbedding(ctx context.Context, msgID MessageID) ([]byte, string, int, bool, error) {
	q := db.New(GetDB())
	row, err := q.GetMessageEmbedding(ctx, int64(msgID))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", 0, false, nil
		}
		return nil, "", 0, false, fmt.Errorf("get embedding: %w", err)
	}
	return row.Vec, row.TextHash, int(row.Dim), true, nil
}

// ListMissingEmbeddings returns up to `limit` archive rows (oldest-completed-first
// reversed) whose embeddings are absent or were generated with a different model.
func ListMissingEmbeddings(ctx context.Context, email, model string, limit int) ([]MissingEmbeddingRow, error) {
	q := db.New(GetDB())
	rows, err := q.ListMissingEmbeddingsForUser(ctx, db.ListMissingEmbeddingsForUserParams{
		UserEmail: sql.NullString{String: email, Valid: true},
		Model:     model,
		Limit:     int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list missing embeddings: %w", err)
	}
	out := make([]MissingEmbeddingRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, MissingEmbeddingRow{
			ID:           MessageID(r.ID),
			Task:         r.Task,
			OriginalText: r.OriginalText,
		})
	}
	return out, nil
}

// CountMissingEmbeddings returns how many archive rows still need embedding work.
// Why: backfill UI / admin endpoint surfaces remaining work so operators know when
// they can stop calling the batch endpoint.
func CountMissingEmbeddings(ctx context.Context, email, model string) (int, error) {
	q := db.New(GetDB())
	n, err := q.CountMissingEmbeddingsForUser(ctx, db.CountMissingEmbeddingsForUserParams{
		UserEmail: sql.NullString{String: email, Valid: true},
		Model:     model,
	})
	if err != nil {
		return 0, fmt.Errorf("count missing embeddings: %w", err)
	}
	return int(n), nil
}

// ListArchiveEmbeddingsPage streams a single page of (id, vec) for cosine top-K.
// Why: caller pages through with offset to keep RAM bounded around 6 MB regardless
// of total archive size — see services/embedding_service.go cosineTopK loop.
func ListArchiveEmbeddingsPage(ctx context.Context, email, model string, limit, offset int) ([]MessageEmbeddingRow, error) {
	q := db.New(GetDB())
	rows, err := q.ListArchiveEmbeddingsPage(ctx, db.ListArchiveEmbeddingsPageParams{
		UserEmail: sql.NullString{String: email, Valid: true},
		Model:     model,
		Limit:     int64(limit),
		Offset:    int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("list archive embeddings: %w", err)
	}
	out := make([]MessageEmbeddingRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, MessageEmbeddingRow{
			MessageID: MessageID(r.MessageID),
			Vec:       r.Vec,
		})
	}
	return out, nil
}

// ArchiveFTSTopIDs returns archive message IDs in BM25 rank order for the user.
// Why: messages_fts is a virtual table created in migrations.go and is invisible
// to sqlc analysis, so this query lives in raw SQL alongside ftsSearchArchivedMessages.
func ArchiveFTSTopIDs(ctx context.Context, email, query string, limit int) ([]MessageID, error) {
	if strings.TrimSpace(query) == "" || limit <= 0 {
		return nil, nil
	}
	fts := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	const sqlText = `
		SELECT m.id
		FROM messages_fts
		JOIN messages m ON m.id = messages_fts.rowid
		WHERE messages_fts MATCH ?1
		  AND (m.user_email = ?2 OR (m.user_email IS NULL AND ?2 = ''))
		  AND m.lifecycle != 'active'
		ORDER BY bm25(messages_fts)
		LIMIT ?3`
	rows, err := GetDB().QueryContext(ctx, sqlText, fts, email, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("fts archive ids: %w", err)
	}
	defer rows.Close()
	var ids []MessageID
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("fts archive ids scan: %w", err)
		}
		ids = append(ids, MessageID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fts archive ids: %w", err)
	}
	return ids, nil
}

// DeleteEmbeddingsByModel wipes every row generated under `model`. Used when
// rotating to a different embedding model so stale vectors don't pollute results.
func DeleteEmbeddingsByModel(ctx context.Context, model string) error {
	q := db.New(GetDB())
	if err := q.DeleteEmbeddingsByModel(ctx, model); err != nil {
		return fmt.Errorf("delete embeddings by model: %w", err)
	}
	return nil
}
