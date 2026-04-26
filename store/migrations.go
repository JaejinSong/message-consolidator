package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/db"
	"message-consolidator/logger"
	"strings"
	"sync"
)

// Why: Guards the fire-and-forget contact_resolution backfill so concurrent runMigrations
// calls (e.g. test reuse, double-init) cannot spawn duplicate goroutines within one process.
// Multi-instance protection still relies on the in-function `count > 0` early-return.
var migrateContactResolutionOnce sync.Once

func createCoreTables(ctx context.Context, q db.DBTX) error {
	queries := db.New(q)
	for _, step := range []struct {
		name string
		fn   func(context.Context) error
	}{
		{"users", queries.CreateUsersTable},
		{"user_aliases", queries.CreateUserAliasesTable},
		{"gmail_tokens", queries.CreateGmailTokensTable},
		{"messages", queries.CreateMessagesTable},
		{"task_translations", queries.CreateTaskTranslationsTable},
		{"tenant_aliases", queries.CreateTenantAliasesTable},
		{"scan_metadata", queries.CreateScanMetadataTable},
		{"slack_threads", queries.CreateSlackThreadsTable},
		{"reports", queries.CreateReportsTable},
		{"report_translations", queries.CreateReportTranslationsTable},
		{"prompt_logs", queries.CreatePromptLogsTable},
		{"ai_inference_logs", queries.CreateAIInferenceLogsTable},
		{"contacts", queries.CreateContactsTable},
		{"identity_merge_history", queries.CreateIdentityMergeHistoryTable},
		{"contact_resolution", queries.CreateContactResolutionTable},
		{"identity_merge_candidates", queries.CreateIdentityMergeCandidatesTable},
		{"token_usage", queries.CreateTokenUsageTable},
		{"telegram_sessions", queries.CreateTelegramSessionsTable},
		{"telegram_credentials", queries.CreateTelegramCredentialsTable},
		{"app_settings", queries.CreateAppSettingsTable},
	} {
		if err := step.fn(ctx); err != nil {
			return fmt.Errorf("failed to create %s table: %w", step.name, err)
		}
	}
	return nil
}

func runMigrations(ctx context.Context, q db.DBTX) error {
	migrateExistingData(ctx, q)
	migrateContactResolutionOnce.Do(func() {
		go migrateContactResolution(ctx)
	})
	return nil
}

// migrateContactResolution rebuilds contact_resolution on first run after the table was introduced.
// Why: spawned as a fire-and-forget goroutine from runMigrations. Captures the *sql.DB once at start
// so a concurrent ResetForTest (test teardown) cannot nil the global mid-execution and panic.
func migrateContactResolution(ctx context.Context) {
	conn := GetDB()
	if conn == nil {
		return
	}

	var count int
	_ = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM contact_resolution").Scan(&count)
	if count > 0 {
		return
	}

	rows, err := conn.QueryContext(ctx, "SELECT DISTINCT tenant_email FROM contacts")
	if err != nil {
		return
	}
	defer rows.Close()

	var tenants []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err == nil {
			tenants = append(tenants, t)
		}
	}
	for _, t := range tenants {
		if err := RebuildContactResolution(ctx, t); err != nil {
			logger.Errorf("[RESOLUTION] rebuild failed for %s: %v", t, err)
		}
	}
	logger.Infof("[RESOLUTION] contact_resolution populated for %d tenants", len(tenants))
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

func migrateExistingData(ctx context.Context, q db.DBTX) {
	// identity_merge_candidates holds only AI-generated proposals; data loss on schema change is acceptable.
	if !tableHasColumn(ctx, q, "identity_merge_candidates", "contact_id_a") {
		_, _ = q.ExecContext(ctx, "DROP TABLE IF EXISTS identity_merge_candidates")
		_ = db.New(q).CreateIdentityMergeCandidatesTable(ctx)
	}
	if !tableHasColumn(ctx, q, "identity_merge_history", "source_contact_id") {
		_, _ = q.ExecContext(ctx, "DROP TABLE IF EXISTS identity_merge_history")
		_ = db.New(q).CreateIdentityMergeHistoryTable(ctx)
	}

	_, _ = q.ExecContext(ctx, "ALTER TABLE identity_merge_candidates ADD COLUMN proposal_group_id TEXT")
	_, _ = q.ExecContext(ctx, "ALTER TABLE identity_merge_candidates ADD COLUMN canonical_name TEXT")

	if !tableHasColumn(ctx, q, "messages", "is_archived") {
		_, _ = q.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN is_archived INTEGER GENERATED ALWAYS AS (
			CASE WHEN is_deleted = 1 OR category = 'merged' OR done = 1 THEN 1 ELSE 0 END
		) VIRTUAL`)
	}

	_, _ = q.ExecContext(ctx, "UPDATE messages SET is_deleted = 0 WHERE is_deleted IS NULL")
	_, _ = q.ExecContext(ctx, "UPDATE messages SET room = 'General' WHERE room IS NULL OR room = ''")
	_, _ = q.ExecContext(ctx, "UPDATE messages SET category = 'todo' WHERE category IN ('waiting', 'promise')")

	if !tableHasColumn(ctx, q, "users", "tg_user_id") {
		_, _ = q.ExecContext(ctx, "ALTER TABLE users ADD COLUMN tg_user_id TEXT DEFAULT ''")
	}

	if !tableHasColumn(ctx, q, "users", "is_admin") {
		_, _ = q.ExecContext(ctx, "ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0")
	}
	// Why: super admin email is hardcoded; backfill flag if the row was created before the column existed.
	_, _ = q.ExecContext(ctx, "UPDATE users SET is_admin = 1 WHERE email = ?1 AND is_admin = 0", SuperAdminEmail)

	migrateTokenUsageBreakdown(ctx, q)
	migrateTokenUsageReportID(ctx, q)
	migrateOriginalTextOrder(ctx, q)
}

// migrateOriginalTextOrder reverses block order in messages.original_text so newest appears first.
// Why: matches the post-2026-04-24 append queries (prepend pattern). Historical rows stored oldest-first.
func migrateOriginalTextOrder(ctx context.Context, q db.DBTX) {
	if tableHasColumn(ctx, q, "messages", "original_text_flipped") {
		return
	}
	if _, err := q.ExecContext(ctx, "ALTER TABLE messages ADD COLUMN original_text_flipped INTEGER DEFAULT 0"); err != nil {
		logger.Errorf("[MIGRATE] original_text flip: add column failed: %v", err)
		return
	}

	rows, err := q.QueryContext(ctx, "SELECT id, original_text FROM messages WHERE original_text LIKE '%' || char(10) || char(10) || '%'")
	if err != nil {
		logger.Errorf("[MIGRATE] original_text flip: query failed: %v", err)
		return
	}
	type pair struct {
		id   int
		text string
	}
	var pending []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.id, &p.text); err == nil {
			pending = append(pending, p)
		}
	}
	rows.Close()

	reversed := 0
	for _, p := range pending {
		blocks := strings.Split(p.text, "\n\n")
		for i, j := 0, len(blocks)-1; i < j; i, j = i+1, j-1 {
			blocks[i], blocks[j] = blocks[j], blocks[i]
		}
		if _, err := q.ExecContext(ctx, "UPDATE messages SET original_text = ?, original_text_flipped = 1 WHERE id = ?", strings.Join(blocks, "\n\n"), p.id); err == nil {
			reversed++
		}
	}
	_, _ = q.ExecContext(ctx, "UPDATE messages SET original_text_flipped = 1 WHERE original_text_flipped = 0")
	logger.Infof("[MIGRATE] original_text order flipped for %d multi-block rows (of %d pending)", reversed, len(pending))
}

// migrateTokenUsageReportID extends token_usage with a report_id column and folds it into the
// composite UNIQUE key so per-report cost can be aggregated. Historical rows back-fill report_id=0
// (un-attributed bucket). SQLite cannot alter UNIQUE in place — full table rebuild.
func migrateTokenUsageReportID(ctx context.Context, q db.DBTX) {
	if tableHasColumn(ctx, q, "token_usage", "report_id") {
		return
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
			prompt_tokens INT DEFAULT 0,
			completion_tokens INT DEFAULT 0,
			total_tokens INT DEFAULT 0,
			call_count INT DEFAULT 0,
			filtered_count INT DEFAULT 0,
			UNIQUE(user_email, date, step, model, source, report_id)
		)`,
		`INSERT INTO token_usage_new (user_email, date, step, model, source, prompt_tokens, completion_tokens, total_tokens, call_count, filtered_count)
		 SELECT user_email, date, step, model, source, prompt_tokens, completion_tokens, total_tokens, call_count, filtered_count FROM token_usage`,
		`DROP TABLE token_usage`,
		`ALTER TABLE token_usage_new RENAME TO token_usage`,
	}
	for _, s := range stmts {
		if _, err := q.ExecContext(ctx, s); err != nil {
			logger.Errorf("[MIGRATE] token_usage report_id rebuild step failed: %v", err)
			return
		}
	}
	logger.Infof("[MIGRATE] token_usage extended with report_id")
}

// migrateTokenUsageBreakdown rebuilds token_usage with step/model/source/call_count columns
// and the new composite UNIQUE key. SQLite cannot alter UNIQUE constraints in place, so we
// copy historical rows into the legacy bucket (step='', model='', source='').
func migrateTokenUsageBreakdown(ctx context.Context, q db.DBTX) {
	if tableHasColumn(ctx, q, "token_usage", "step") {
		return
	}
	stmts := []string{
		`CREATE TABLE token_usage_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_email VARCHAR(255) NOT NULL,
			date DATE NOT NULL DEFAULT (date('now')),
			step TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			prompt_tokens INT DEFAULT 0,
			completion_tokens INT DEFAULT 0,
			total_tokens INT DEFAULT 0,
			call_count INT DEFAULT 0,
			filtered_count INT DEFAULT 0,
			UNIQUE(user_email, date, step, model, source)
		)`,
		`INSERT INTO token_usage_new (user_email, date, prompt_tokens, completion_tokens, total_tokens, filtered_count)
		 SELECT user_email, date, prompt_tokens, completion_tokens, total_tokens, filtered_count FROM token_usage`,
		`DROP TABLE token_usage`,
		`ALTER TABLE token_usage_new RENAME TO token_usage`,
	}
	for _, s := range stmts {
		if _, err := q.ExecContext(ctx, s); err != nil {
			logger.Errorf("[MIGRATE] token_usage rebuild step failed: %v", err)
			return
		}
	}
	logger.Infof("[MIGRATE] token_usage extended with step/model/source/call_count")
}

func tableHasColumn(ctx context.Context, q db.DBTX, table, column string) bool {
	rows, err := q.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			continue
		}
		if name == column {
			return true
		}
	}
	return false
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
	}
	for _, ddl := range indexes {
		_, _ = q.ExecContext(ctx, ddl)
	}
}
