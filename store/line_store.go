package store

import (
	"context"

	"message-consolidator/db"
)

// InsertLineInbox persists a raw LINE webhook message for deferred scanner processing.
func InsertLineInbox(ctx context.Context, p db.InsertLineInboxParams) error {
	return WithDBRetry("InsertLineInbox", func() error {
		return db.New(GetDB()).InsertLineInbox(ctx, p)
	})
}

// GetUnprocessedLineMessages returns unprocessed rows from line_inbox, ordered oldest-first.
func GetUnprocessedLineMessages(ctx context.Context) ([]db.LineInbox, error) {
	var rows []db.LineInbox
	err := WithDBRetry("GetUnprocessedLineMessages", func() error {
		var e error
		rows, e = db.New(GetDB()).GetUnprocessedLineMessages(ctx)
		return e
	})
	return rows, err
}

// MarkLineInboxProcessed marks a single row as processed so it is not re-scanned.
func MarkLineInboxProcessed(ctx context.Context, id int64) error {
	return WithDBRetry("MarkLineInboxProcessed", func() error {
		return db.New(GetDB()).MarkLineInboxProcessed(ctx, id)
	})
}
