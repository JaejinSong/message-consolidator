-- Consolidated Schema for sqlc (SQLite)
-- NOTE: CREATE INDEX statements are stripped by sqlc and must be defined in createIndexes() in migrations.go.

-- name: CreateUsersTable :exec
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT UNIQUE,
    name TEXT,
    slack_id TEXT,
    wa_jid TEXT,
    picture TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- name: CreateUserAliasesTable :exec
CREATE TABLE IF NOT EXISTS user_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    alias_name TEXT NOT NULL,
    UNIQUE(user_id, alias_name)
);

-- name: CreateGmailTokensTable :exec
CREATE TABLE IF NOT EXISTS gmail_tokens (
    user_email TEXT PRIMARY KEY,
    token_json TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- name: CreateMessagesTable :exec
CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_email TEXT,
    source TEXT,
    room TEXT,
    task TEXT,
    requester TEXT,
    assignee TEXT,
    assigned_at DATETIME,
    link TEXT,
    source_ts TEXT,
    pinned BOOLEAN DEFAULT FALSE,
    original_text TEXT,
    done BOOLEAN DEFAULT 0,
    is_deleted BOOLEAN DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    category TEXT DEFAULT 'todo',
    deadline TEXT,
    thread_id TEXT,
    assignee_reason TEXT,
    replied_to_id TEXT,
    is_context_query INTEGER DEFAULT 0,
    constraints TEXT DEFAULT '[]',
    metadata TEXT DEFAULT '{}',
    source_channels TEXT DEFAULT '[]',
    consolidated_context TEXT DEFAULT '[]',
    subtasks TEXT DEFAULT '[]',
    UNIQUE(user_email, source_ts)
);

-- name: CreateTaskTranslationsTable :exec
CREATE TABLE IF NOT EXISTS task_translations (
    message_id INTEGER REFERENCES messages(id) ON DELETE CASCADE,
    language_code TEXT NOT NULL,
    language_deprecated TEXT,
    translated_text TEXT NOT NULL,
    PRIMARY KEY (message_id, language_code)
);

-- name: CreateTenantAliasesTable :exec
CREATE TABLE IF NOT EXISTS tenant_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_email TEXT NOT NULL,
    original_name TEXT NOT NULL,
    primary_name TEXT NOT NULL,
    UNIQUE(user_email, original_name)
);

-- name: CreateScanMetadataTable :exec
CREATE TABLE IF NOT EXISTS scan_metadata (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_email TEXT NOT NULL,
    source TEXT NOT NULL,
    target_id TEXT NOT NULL,
    last_ts TEXT,
    UNIQUE(user_email, source, target_id)
);

-- name: CreateContactsTable :exec
CREATE TABLE IF NOT EXISTS contacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_email VARCHAR(255) NOT NULL,
    canonical_id VARCHAR(255) NOT NULL,
    display_name VARCHAR(255) NOT NULL,
    source VARCHAR(50) DEFAULT 'all',
    master_contact_id INTEGER REFERENCES contacts(id),
    contact_type TEXT DEFAULT 'none',
    secondary_ids TEXT DEFAULT '[]',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_email, canonical_id)
);

-- name: CreateContactResolutionTable :exec
CREATE TABLE IF NOT EXISTS contact_resolution (
    tenant_email TEXT NOT NULL,
    raw_identifier TEXT NOT NULL,
    contact_id   INTEGER NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    PRIMARY KEY (tenant_email, raw_identifier)
);

-- name: CreateIdentityMergeHistoryTable :exec
CREATE TABLE IF NOT EXISTS identity_merge_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_contact_id INTEGER NOT NULL REFERENCES contacts(id),
    target_contact_id INTEGER NOT NULL REFERENCES contacts(id),
    reason TEXT NOT NULL,
    merged_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- name: CreateIdentityMergeCandidatesTable :exec
CREATE TABLE IF NOT EXISTS identity_merge_candidates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    contact_id_a INTEGER NOT NULL REFERENCES contacts(id),
    contact_id_b INTEGER NOT NULL REFERENCES contacts(id),
    confidence REAL NOT NULL,
    reason TEXT,
    status TEXT DEFAULT 'pending',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(contact_id_a, contact_id_b)
);

-- name: CreateAIInferenceLogsTable :exec
CREATE TABLE IF NOT EXISTS ai_inference_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER REFERENCES messages(id),
    source TEXT,
    original_text TEXT,
    raw_response TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- name: CreatePromptLogsTable :exec
CREATE TABLE IF NOT EXISTS prompt_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    model TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- name: CreateReportsTable :exec
CREATE TABLE IF NOT EXISTS reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_email TEXT NOT NULL,
    start_date TEXT NOT NULL,
    end_date TEXT NOT NULL,
    visualization TEXT NOT NULL,
    status TEXT DEFAULT 'completed',
    is_truncated INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- name: CreateReportTranslationsTable :exec
CREATE TABLE IF NOT EXISTS report_translations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id INTEGER NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
    language_code TEXT NOT NULL,
    language_deprecated TEXT,
    summary TEXT NOT NULL,
    UNIQUE(report_id, language_code)
);

-- name: CreateSlackThreadsTable :exec
CREATE TABLE IF NOT EXISTS slack_threads (
    channel_id TEXT,
    thread_ts TEXT,
    last_reply_ts TEXT,
    last_activity_ts TEXT,
    status TEXT DEFAULT 'active',
    user_email TEXT,
    PRIMARY KEY (channel_id, thread_ts, user_email)
);

-- name: CreateTokenUsageTable :exec
CREATE TABLE IF NOT EXISTS token_usage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_email VARCHAR(255) NOT NULL,
    date DATE NOT NULL DEFAULT (date('now')),
    prompt_tokens INT DEFAULT 0,
    completion_tokens INT DEFAULT 0,
    total_tokens INT DEFAULT 0,
    filtered_count INT DEFAULT 0,
    UNIQUE(user_email, date)
);


-- Views
-- name: CreateContactsResolvedView :exec
CREATE VIEW IF NOT EXISTS v_contacts_resolved AS
SELECT
    c.id,
    c.tenant_email,
    c.canonical_id AS original_canonical_id,
    c.display_name AS original_display_name,
    COALESCE(m.canonical_id, c.canonical_id) AS effective_canonical_id,
    COALESCE(m.display_name, c.display_name) AS effective_display_name,
    COALESCE(m.contact_type, c.contact_type, 'none') AS contact_type,
    CASE WHEN c.master_contact_id IS NOT NULL THEN 1 ELSE 0 END AS is_merged,
    COALESCE(c.source, 'all') AS original_source
FROM contacts c
LEFT JOIN contacts m ON c.master_contact_id = m.id AND c.tenant_email = m.tenant_email;

-- name: CreateMessagesView :exec
CREATE VIEW IF NOT EXISTS v_messages AS
SELECT
    m.id,
    COALESCE(m.user_email, '') as user_email,
    COALESCE(m.source, '') as source,
    COALESCE(m.room, '') as room,
    COALESCE(m.task, '') as task,
    COALESCE(cr_req.effective_display_name, m.requester, '') as requester,
    COALESCE(cr_asg.effective_display_name, m.assignee, '') as assignee,
    m.assigned_at,
    COALESCE(m.link, '') as link,
    COALESCE(m.source_ts, '') as source_ts,
    COALESCE(m.pinned, 0) as pinned,
    COALESCE(m.original_text, '') as original_text,
    COALESCE(m.done, 0) as done,
    COALESCE(m.is_deleted, 0) as is_deleted,
    m.created_at,
    m.completed_at,
    COALESCE(m.category, 'todo') as category,
    COALESCE(m.deadline, '') as deadline,
    COALESCE(m.thread_id, '') as thread_id,
    COALESCE(m.assignee_reason, '') as assignee_reason,
    COALESCE(m.replied_to_id, '') as replied_to_id,
    COALESCE(m.is_context_query, 0) as is_context_query,
    COALESCE(m.constraints, '[]') as constraints,
    COALESCE(m.metadata, '{}') as metadata,
    COALESCE(m.source_channels, '[]') as source_channels,
    COALESCE(m.consolidated_context, '[]') as consolidated_context,
    COALESCE(m.subtasks, '[]') as subtasks,
    COALESCE(cr_req.effective_canonical_id, m.requester, '') as requester_canonical,
    COALESCE(cr_asg.effective_canonical_id, m.assignee, '') as assignee_canonical,
    COALESCE(cr_req.contact_type, 'none') as requester_type,
    COALESCE(cr_asg.contact_type, 'none') as assignee_type
FROM messages m
LEFT JOIN v_contacts_resolved cr_req ON m.user_email = cr_req.tenant_email AND m.requester = cr_req.original_canonical_id
LEFT JOIN v_contacts_resolved cr_asg ON m.user_email = cr_asg.tenant_email AND m.assignee = cr_asg.original_canonical_id;
