package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"message-consolidator/db"
)

// schemaVersion gates DDL replay on startup. Bump whenever this file changes
// (new tables, view rebuild logic, indexes, FTS) so existing prod DBs re-run
// migrations on next deploy. Stored in app_settings under key "schema_version".
const schemaVersion = 18

func schemaIsCurrent(ctx context.Context, dbConn *sql.DB) bool {
	queries := db.New(dbConn)
	row, err := queries.GetAppSetting(ctx, "schema_version")
	if err != nil {
		return false
	}
	return row.Value == strconv.Itoa(schemaVersion)
}

func stampSchemaVersion(ctx context.Context, q db.DBTX) error {
	queries := db.New(q)
	return queries.UpsertAppSetting(ctx, db.UpsertAppSettingParams{
		Key:       "schema_version",
		Value:     strconv.Itoa(schemaVersion),
		UpdatedBy: "system",
	})
}

func createCoreTables(ctx context.Context, q db.DBTX) error {
	queries := db.New(q)
	for _, step := range []struct {
		name string
		fn   func(context.Context) error
	}{
		{"users", queries.CreateUsersTable},
		{"user_aliases", queries.CreateUserAliasesTable},
		{"gmail_tokens", queries.CreateGmailTokensTable},
		{"sessions", queries.CreateSessionsTable},
		{"messages", queries.CreateMessagesTable},
		{"task_translations", queries.CreateTaskTranslationsTable},
		{"tenant_aliases", queries.CreateTenantAliasesTable},
		{"scan_metadata", queries.CreateScanMetadataTable},
		{"slack_threads", queries.CreateSlackThreadsTable},
		{"reports", queries.CreateReportsTable},
		{"report_translations", queries.CreateReportTranslationsTable},
		{"ai_inference_logs", queries.CreateAIInferenceLogsTable},
		{"contacts", queries.CreateContactsTable},
		{"identity_merge_history", queries.CreateIdentityMergeHistoryTable},
		{"contact_resolution", queries.CreateContactResolutionTable},
		{"identity_merge_candidates", queries.CreateIdentityMergeCandidatesTable},
		{"token_usage", queries.CreateTokenUsageTable},
		{"telegram_sessions", queries.CreateTelegramSessionsTable},
		{"telegram_credentials", queries.CreateTelegramCredentialsTable},
		{"app_settings", queries.CreateAppSettingsTable},
		{"message_embeddings", queries.CreateMessageEmbeddingsTable},
		{"task_grants", queries.CreateTaskGrantsTable},
		{"wa_messages", queries.CreateWAMessagesTable},
		{"line_inbox", queries.CreateLineInboxTable},
		{"learned_examples", queries.CreateLearnedExamplesTable},
		{"correction_observations", queries.CreateCorrectionObservationsTable},
	} {
		if err := step.fn(ctx); err != nil {
			return fmt.Errorf("failed to create %s table: %w", step.name, err)
		}
	}
	if err := createMessagesFTS(ctx, q); err != nil {
		return fmt.Errorf("failed to create messages_fts: %w", err)
	}
	return nil
}

// createMessagesFTS provisions the fts5 virtual table over messages plus the three
// sync triggers. Why: kept out of sqlc because sqlc-sqlite truncates trigger bodies
// at the first ';' inside BEGIN…END, so each trigger would lose its tail statement.
// IF NOT EXISTS makes every step idempotent across cold starts and existing prod DBs.
func createMessagesFTS(ctx context.Context, q db.DBTX) error {
	stmts := []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
			task, original_text, requester, assignee,
			content='messages', content_rowid='id',
			tokenize='trigram case_sensitive 0'
		)`,
		`CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
			INSERT INTO messages_fts(rowid, task, original_text, requester, assignee)
			VALUES (new.id, new.task, new.original_text, new.requester, new.assignee);
		END`,
		`CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, task, original_text, requester, assignee)
			VALUES ('delete', old.id, old.task, old.original_text, old.requester, old.assignee);
		END`,
		`CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, task, original_text, requester, assignee)
			VALUES ('delete', old.id, old.task, old.original_text, old.requester, old.assignee);
			INSERT INTO messages_fts(rowid, task, original_text, requester, assignee)
			VALUES (new.id, new.task, new.original_text, new.requester, new.assignee);
		END`,
	}
	for _, s := range stmts {
		if _, err := q.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func rebuildViews(ctx context.Context, q db.DBTX) error {
	queries := db.New(q)
	_, _ = q.ExecContext(ctx, "DROP VIEW IF EXISTS v_contacts_resolved")
	if err := queries.CreateContactsResolvedView(ctx); err != nil {
		return fmt.Errorf("failed to create v_contacts_resolved: %w", err)
	}
	_, _ = q.ExecContext(ctx, "DROP VIEW IF EXISTS v_messages")
	if err := queries.CreateMessagesView(ctx); err != nil {
		return fmt.Errorf("failed to create v_messages: %w", err)
	}
	return nil
}

func createIndexes(ctx context.Context, q db.DBTX) {
	indexes := []string{
		// user_aliases
		"CREATE INDEX IF NOT EXISTS idx_user_aliases_user_id ON user_aliases(user_id)",
		// messages
		"CREATE INDEX IF NOT EXISTS idx_messages_dashboard_filter ON messages(user_email, is_deleted, done, category, assignee)",
		"CREATE INDEX IF NOT EXISTS idx_messages_task ON messages(task)",
		"CREATE INDEX IF NOT EXISTS idx_messages_room ON messages(room)",
		"CREATE INDEX IF NOT EXISTS idx_messages_requester ON messages(requester)",
		"CREATE INDEX IF NOT EXISTS idx_messages_assignee ON messages(assignee)",
		"CREATE INDEX IF NOT EXISTS idx_messages_source ON messages(source)",
		"CREATE INDEX IF NOT EXISTS idx_messages_is_deleted ON messages(is_deleted)",
		"CREATE INDEX IF NOT EXISTS idx_messages_completed_at ON messages(completed_at)",
		"CREATE INDEX IF NOT EXISTS idx_messages_user_done_completed ON messages(user_email, done, completed_at)",
		// contacts
		"CREATE INDEX IF NOT EXISTS idx_contacts_canonical ON contacts(canonical_id)",
		"CREATE INDEX IF NOT EXISTS idx_contacts_tenant_canonical ON contacts(tenant_email, canonical_id)",
		"CREATE INDEX IF NOT EXISTS idx_contacts_tenant_display_name ON contacts(tenant_email, LOWER(display_name))",
		// slack_threads
		"CREATE INDEX IF NOT EXISTS idx_slack_threads_status ON slack_threads(status)",
		// archive: is_archived narrows to the full set, done/is_deleted cover status filtering
		"CREATE INDEX IF NOT EXISTS idx_messages_archive_filter ON messages(user_email, is_archived, done, is_deleted)",
		// excluded: partial index keeps the excluded list/digest scans off the main table scan
		"CREATE INDEX IF NOT EXISTS idx_messages_excluded ON messages(user_email, excluded_at) WHERE excluded_at IS NOT NULL",
		// message_embeddings: SemanticTopK JOINs by model then message_id.
		"CREATE INDEX IF NOT EXISTS idx_msg_emb_model_id ON message_embeddings(model, message_id)",
		"CREATE INDEX IF NOT EXISTS idx_task_grants_grantor ON task_grants(grantor_user_id)",
		"CREATE INDEX IF NOT EXISTS idx_task_grants_grantee ON task_grants(grantee_user_id)",
		// wa_messages
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_wa_messages_message_id ON wa_messages(message_id)",
		"CREATE INDEX IF NOT EXISTS idx_wa_messages_ts ON wa_messages(ts)",
		"CREATE INDEX IF NOT EXISTS idx_wa_messages_chat_jid_ts ON wa_messages(chat_jid, ts)",
		// learned_examples / correction_observations
		"CREATE INDEX IF NOT EXISTS idx_learned_examples_user ON learned_examples(user_email)",
		"CREATE INDEX IF NOT EXISTS idx_correction_obs_user_status ON correction_observations(user_email, status)",
	}
	for _, ddl := range indexes {
		_, _ = q.ExecContext(ctx, ddl)
	}
}

// reindexWAMessages drops the initial over-broad indexes created in v8 and replaces them
// with query-aligned ones. Why: the original (email, ts) composite cannot satisfy ts-only
// date queries, and the (chat_jid) single-column index misses the sort column.
// DROP IF EXISTS makes this idempotent — no-op when indexes never existed.
func reindexWAMessages(ctx context.Context, q db.DBTX) {
	drops := []string{
		"DROP INDEX IF EXISTS idx_wa_messages_email_ts",
		"DROP INDEX IF EXISTS idx_wa_messages_chat_jid",
	}
	for _, ddl := range drops {
		_, _ = q.ExecContext(ctx, ddl)
	}
}

// addThinkingTokensColumn adds thinking_tokens to token_usage on existing DBs.
// Why: SQLite does not support IF NOT EXISTS for ALTER TABLE; column existence check makes it idempotent.
func addThinkingTokensColumn(ctx context.Context, q db.DBTX) error {
	var has int
	_ = q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('token_usage') WHERE name='thinking_tokens'`,
	).Scan(&has)
	if has > 0 {
		return nil
	}
	if _, err := q.ExecContext(ctx,
		`ALTER TABLE token_usage ADD COLUMN thinking_tokens INT DEFAULT 0`,
	); err != nil {
		return fmt.Errorf("add thinking_tokens column: %w", err)
	}
	return nil
}

// addCachedTokensColumn adds cached_tokens to token_usage on existing DBs.
// Why: DeepSeek prompt-cache hits are billed at ~1/50 the input rate; storing the hit
// count lets the cost dashboard price them separately. SQLite lacks IF NOT EXISTS for
// ALTER TABLE, so the pragma check makes this idempotent.
func addCachedTokensColumn(ctx context.Context, q db.DBTX) error {
	var has int
	_ = q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('token_usage') WHERE name='cached_tokens'`,
	).Scan(&has)
	if has > 0 {
		return nil
	}
	if _, err := q.ExecContext(ctx,
		`ALTER TABLE token_usage ADD COLUMN cached_tokens INT DEFAULT 0`,
	); err != nil {
		return fmt.Errorf("add cached_tokens column: %w", err)
	}
	return nil
}

// migrateTokenUsagePeak (v17) rebuilds token_usage with a peak column folded into the
// UNIQUE key. Why: DeepSeek bills its peak window (UTC 01-04 and 06-10, Mon-Fri) at 2x the
// off-peak rate, and token_usage aggregates per day, so the window has to be recorded at
// write time. SQLite cannot add a column to an existing UNIQUE constraint, so the only
// path is CREATE/INSERT/DROP/RENAME.
// Existing rows carry peak=0: their window is unrecoverable from date alone, and off-peak
// is the rate they were already being shown at, so nothing is retroactively repriced.
// Idempotent: skipped once the peak column exists.
func migrateTokenUsagePeak(ctx context.Context, q db.DBTX) error {
	var has int
	_ = q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('token_usage') WHERE name='peak'`,
	).Scan(&has)
	if has > 0 {
		return nil
	}
	stmts := []string{
		`CREATE TABLE token_usage_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_email VARCHAR(255) NOT NULL,
			date DATE NOT NULL DEFAULT (date('now')),
			step TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			report_id INTEGER NOT NULL DEFAULT 0,
			peak INTEGER NOT NULL DEFAULT 0,
			prompt_tokens INT DEFAULT 0,
			completion_tokens INT DEFAULT 0,
			thinking_tokens INT DEFAULT 0,
			cached_tokens INT DEFAULT 0,
			total_tokens INT DEFAULT 0,
			call_count INT DEFAULT 0,
			filtered_count INT DEFAULT 0,
			UNIQUE(user_email, date, step, model, source, report_id, peak)
		)`,
		`INSERT INTO token_usage_new (id, user_email, date, step, model, source, report_id, peak,
			prompt_tokens, completion_tokens, thinking_tokens, cached_tokens, total_tokens, call_count, filtered_count)
			SELECT id, user_email, date, step, model, source, report_id, 0,
			       prompt_tokens, completion_tokens, thinking_tokens, cached_tokens, total_tokens, call_count, filtered_count
			FROM token_usage`,
		`DROP TABLE token_usage`,
		`ALTER TABLE token_usage_new RENAME TO token_usage`,
	}
	for _, stmt := range stmts {
		if _, err := q.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate token_usage peak: %w", err)
		}
	}
	return nil
}

// addMessagesUpdatedAtColumn adds updated_at to messages on existing DBs.
// Why: SQLite does not support IF NOT EXISTS for ALTER TABLE, so we check pragma_table_info first.
// Safe to re-run — idempotent via the column existence check.
func addMessagesUpdatedAtColumn(ctx context.Context, q db.DBTX) error {
	var has int
	_ = q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name='updated_at'`,
	).Scan(&has)
	if has > 0 {
		return nil
	}
	if _, err := q.ExecContext(ctx,
		`ALTER TABLE messages ADD COLUMN updated_at DATETIME NOT NULL DEFAULT '1970-01-01T00:00:00Z'`,
	); err != nil {
		return fmt.Errorf("add updated_at column: %w", err)
	}
	// Why: ALTER TABLE DEFAULT fills existing rows with the ALTER execution time, not
	// created_at. Backfill so "no update" rows show their registration date correctly.
	if _, err := q.ExecContext(ctx,
		`UPDATE messages SET updated_at = created_at WHERE updated_at <= '1970-01-02' OR updated_at >= datetime('now', '-1 minute')`,
	); err != nil {
		return fmt.Errorf("backfill updated_at: %w", err)
	}
	return nil
}

// addDeadlineColumns adds deadline_date and deadline_inferred to messages on existing DBs.
// Why: ISO-normalized deadline enables reliable date comparisons; inferred flag distinguishes AI-derived dates.
// Idempotent via pragma_table_info existence check (SQLite does not support IF NOT EXISTS on ALTER TABLE).
func addDeadlineColumns(ctx context.Context, q db.DBTX) error {
	for _, col := range []struct {
		name string
		ddl  string
	}{
		{"deadline_date", "ALTER TABLE messages ADD COLUMN deadline_date DATE"},
		{"deadline_inferred", "ALTER TABLE messages ADD COLUMN deadline_inferred INTEGER DEFAULT 0"},
	} {
		var has int
		_ = q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name=?`, col.name,
		).Scan(&has)
		if has > 0 {
			continue
		}
		if _, err := q.ExecContext(ctx, col.ddl); err != nil {
			return fmt.Errorf("add messages.%s: %w", col.name, err)
		}
	}
	return nil
}

// suppressOldUndatedNudges marks pre-existing undated PROMISE/WAITING items older than 14 days
// as already-nudged so the first deploy does not spam users with backlog notifications.
// Items created within the last 14 days enter the normal aging flow and receive nudges naturally.
// Idempotent: skips rows that already have reminded_at_undated_d3 set.
func suppressOldUndatedNudges(ctx context.Context, q db.DBTX) error {
	const suppressSQL = `
UPDATE messages
SET metadata = json_set(
    COALESCE(NULLIF(metadata, ''), '{}'),
    '$.reminded_at_undated_d3',  'suppressed',
    '$.reminded_at_undated_d7',  'suppressed',
    '$.reminded_at_undated_d14', 'suppressed'
)
WHERE category IN ('PROMISE', 'WAITING')
  AND done = 0
  AND is_deleted = 0
  AND (deadline IS NULL OR deadline = '')
  AND json_extract(COALESCE(NULLIF(metadata, ''), '{}'), '$.reminded_at_undated_d3') IS NULL
  AND created_at < datetime('now', '-14 days')`
	if _, err := q.ExecContext(ctx, suppressSQL); err != nil {
		return fmt.Errorf("suppress old undated nudges: %w", err)
	}
	return nil
}

// dropAIInferencePayloadColumns removes original_text and raw_response from ai_inference_logs.
// Why: payload is redundant — logger/ai_logger.go already writes it to /app/logs/ai_inference.log
// which WhaTap logsink collects. Removing from DB eliminates the largest per-call Bytes Synced chunk.
// SQLite 3.35+ supports ALTER TABLE DROP COLUMN for columns without indexes or FK references.
// Idempotent: skipped when columns are already absent.
func dropAIInferencePayloadColumns(ctx context.Context, q db.DBTX) error {
	for _, col := range []string{"original_text", "raw_response"} {
		var has int
		_ = q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_table_info('ai_inference_logs') WHERE name=?`, col,
		).Scan(&has)
		if has == 0 {
			continue
		}
		if _, err := q.ExecContext(ctx,
			`ALTER TABLE ai_inference_logs DROP COLUMN `+col,
		); err != nil {
			return fmt.Errorf("drop ai_inference_logs.%s: %w", col, err)
		}
	}
	return nil
}

// backfillZeroTimeAssignedAt repairs task rows whose assigned_at is missing (NULL) or
// Go zero time ('0001-01-01…'), setting it to created_at. Why: the Gmail completion path
// (handleThreadActivity) persisted new tasks without an envelope timestamp, landing 353
// rows with assigned_at=NULL — which silently disabled aging/deadline/stalled automation
// that keys off assigned_at. created_at is the best available proxy for the assignment
// time. Idempotent: repaired rows no longer match the WHERE clause on re-run.
func backfillZeroTimeAssignedAt(ctx context.Context, q db.DBTX) error {
	const backfillSQL = `
UPDATE messages
SET assigned_at = created_at
WHERE task IS NOT NULL AND task != ''
  AND (assigned_at IS NULL OR assigned_at < '1970-01-01')`
	if _, err := q.ExecContext(ctx, backfillSQL); err != nil {
		return fmt.Errorf("backfill zero-time assigned_at: %w", err)
	}
	return nil
}

// backfillWhatsAppThreadIDs (v16) anchors legacy WhatsApp tasks on their source message ID.
// Why: WA tasks were saved with thread_id=” (no saveThreadAnchor), so quote-reply
// completion lookups (GetIncompleteByThreadID) and the findMatch cross-thread merge guard
// never matched. source_ts holds the originating WA message ID — the same anchor
// SaveThreadID now writes. Done rows are included so HasAnyTaskInThread stays consistent.
// Idempotent: repaired rows no longer match the WHERE clause on re-run.
func backfillWhatsAppThreadIDs(ctx context.Context, q db.DBTX) error {
	const backfillSQL = `
UPDATE messages
SET thread_id = source_ts
WHERE source = 'whatsapp'
  AND COALESCE(thread_id, '') = ''
  AND COALESCE(source_ts, '') != ''`
	if _, err := q.ExecContext(ctx, backfillSQL); err != nil {
		return fmt.Errorf("backfill whatsapp thread_id: %w", err)
	}
	return nil
}

// migrateLifecycleExcluded (v15) adds excluded_at and rebuilds the lifecycle generated
// column with an 'excluded' branch (long-term-unprocessed tasks parked out of tracking).
// Why: SQLite cannot ALTER a generated column's expression; the only path is DROP + re-ADD
// (both supported for VIRTUAL columns on SQLite 3.35+/libSQL). v_messages references
// lifecycle so it is dropped first; rebuildViews recreates it later in the same tx.
// Idempotent: skipped when the messages DDL already contains the 'excluded' branch.
func migrateLifecycleExcluded(ctx context.Context, q db.DBTX) error {
	var ddl string
	_ = q.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='messages'`,
	).Scan(&ddl)
	if strings.Contains(ddl, "'excluded'") {
		return nil
	}

	var has int
	_ = q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name='excluded_at'`,
	).Scan(&has)
	if has == 0 {
		if _, err := q.ExecContext(ctx,
			`ALTER TABLE messages ADD COLUMN excluded_at DATETIME`,
		); err != nil {
			return fmt.Errorf("add excluded_at column: %w", err)
		}
	}

	stmts := []string{
		`DROP VIEW IF EXISTS v_messages`,
		`ALTER TABLE messages DROP COLUMN lifecycle`,
		`ALTER TABLE messages ADD COLUMN lifecycle TEXT GENERATED ALWAYS AS (
			CASE
				WHEN category = 'merged'             THEN 'merged'
				WHEN done = 0 AND is_deleted = 1     THEN 'canceled'
				WHEN done = 1 AND is_deleted = 1     THEN 'swept'
				WHEN done = 1                        THEN 'done'
				WHEN excluded_at IS NOT NULL         THEN 'excluded'
				ELSE 'active'
			END
		) VIRTUAL`,
	}
	for _, s := range stmts {
		if _, err := q.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("rebuild lifecycle with excluded branch: %w", err)
		}
	}
	return nil
}

// migrateEmbeddingsToF32 atomically recreates message_embeddings with vec typed
// as F32_BLOB(768) so libsql can run vector_distance_cos on the server side.
// Why: SQLite cannot ALTER a column's type; the only path is CREATE/INSERT/DROP/RENAME.
// The existing little-endian float32 bytes are copied 1:1 because F32_BLOB stores
// the same raw encoding that Float32sToBytes produces.
// Idempotent: skipped when the table already declares F32_BLOB.
func migrateEmbeddingsToF32(ctx context.Context, q db.DBTX) error {
	var count int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='message_embeddings'`,
	).Scan(&count); err != nil || count == 0 {
		return err
	}
	var ddl string
	_ = q.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE name='message_embeddings'`,
	).Scan(&ddl)
	if strings.Contains(ddl, "F32_BLOB") {
		return nil
	}
	stmts := []string{
		`CREATE TABLE message_embeddings_new (
			message_id INTEGER PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
			model TEXT NOT NULL, dim INTEGER NOT NULL,
			vec F32_BLOB(768) NOT NULL, text_hash TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`INSERT INTO message_embeddings_new
			SELECT message_id, model, dim, vec, text_hash, created_at
			FROM message_embeddings WHERE dim = 768`,
		`DROP TABLE message_embeddings`,
		`ALTER TABLE message_embeddings_new RENAME TO message_embeddings`,
	}
	for _, s := range stmts {
		if _, err := q.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("migrate embeddings to F32_BLOB: %w", err)
		}
	}
	return nil
}
