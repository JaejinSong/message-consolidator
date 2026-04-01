-- name: CreateUsersTable :exec
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT UNIQUE,
    name TEXT,
    slack_id TEXT,
    wa_jid TEXT,
    picture TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    points INTEGER DEFAULT 0,
    streak INTEGER DEFAULT 0,
    level INTEGER DEFAULT 1,
    xp INTEGER DEFAULT 0,
    daily_goal INTEGER DEFAULT 5,
    last_completed_at DATETIME,
    streak_freezes INTEGER DEFAULT 0
);

-- name: CreateUserAliasesTable :exec
CREATE TABLE IF NOT EXISTS user_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER REFERENCES users(id),
    alias_name TEXT,
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
    metadata TEXT DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_thread_id ON messages(thread_id);

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

-- name: CreateAchievementsTable :exec
CREATE TABLE IF NOT EXISTS achievements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT,
    icon TEXT,
    criteria_type TEXT,
    criteria_value INTEGER,
    target_value INTEGER DEFAULT 0,
    xp_reward INTEGER DEFAULT 0
);

-- name: CreateUserAchievementsTable :exec
CREATE TABLE IF NOT EXISTS user_achievements (
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    achievement_id INTEGER REFERENCES achievements(id) ON DELETE CASCADE,
    unlocked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, achievement_id)
);

-- name: CreateContactsTable :exec
CREATE TABLE IF NOT EXISTS contacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_email VARCHAR(255) NOT NULL,
    canonical_id VARCHAR(255) NOT NULL, -- Normalized lowercase email (SSOT)
    display_name VARCHAR(255) NOT NULL,
    aliases TEXT NOT NULL DEFAULT '', -- Comma-separated variation names
    source VARCHAR(50) DEFAULT 'all',
    master_contact_id INTEGER REFERENCES contacts(id), -- Unified Account Reference
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_email, canonical_id)
);
CREATE INDEX IF NOT EXISTS idx_contacts_canonical ON contacts(canonical_id);
CREATE INDEX IF NOT EXISTS idx_contacts_tenant_canonical ON contacts(tenant_email, canonical_id);

-- name: CreateContactsResolvedView :exec
DROP VIEW IF EXISTS v_contacts_resolved;
CREATE VIEW v_contacts_resolved AS
SELECT 
    c.id,
    c.tenant_email,
    c.canonical_id AS original_canonical_id,
    c.display_name AS original_display_name,
    COALESCE(m.canonical_id, c.canonical_id) AS effective_canonical_id,
    COALESCE(m.display_name, c.display_name) AS effective_display_name,
    CASE WHEN c.master_contact_id IS NOT NULL THEN 1 ELSE 0 END AS is_merged,
    c.source AS original_source
FROM contacts c
LEFT JOIN contacts m ON c.master_contact_id = m.id AND c.tenant_email = m.tenant_email;


-- name: CreateMessagesView :exec
DROP VIEW IF EXISTS v_messages;
CREATE VIEW v_messages AS
SELECT 
    m.id, 
    m.user_email, 
    m.source, 
    COALESCE(m.room, '') as room, 
    m.task, 
    COALESCE(cr_req.effective_display_name, m.requester) as requester, 
    COALESCE(cr_asg.effective_display_name, m.assignee, '') as assignee,
    m.assigned_at,
    m.link, 
    m.source_ts, 
    COALESCE(m.original_text, '') as original_text, 
    m.done, 
    m.is_deleted, 
    m.created_at, 
    m.completed_at, 
    COALESCE(m.category, 'todo') as category, 
    COALESCE(m.deadline, '') as deadline,
    COALESCE(m.thread_id, '') as thread_id,
    COALESCE(m.assignee_reason, '') as assignee_reason,
    COALESCE(m.replied_to_id, '') as replied_to_id,
    m.is_context_query,
    m.metadata,
    COALESCE(cr_req.effective_canonical_id, m.requester) as requester_canonical,
    COALESCE(cr_asg.effective_canonical_id, m.assignee) as assignee_canonical
FROM messages m
LEFT JOIN v_contacts_resolved cr_req ON m.user_email = cr_req.tenant_email AND m.requester = cr_req.original_canonical_id
LEFT JOIN v_contacts_resolved cr_asg ON m.user_email = cr_asg.tenant_email AND m.assignee = cr_asg.original_canonical_id;

-- name: CreateUsersView :exec
DROP VIEW IF EXISTS v_users;
CREATE VIEW v_users AS
SELECT 
    id, 
    email, 
    COALESCE(name, '') as name, 
    COALESCE(slack_id, '') as slack_id, 
    COALESCE(wa_jid, '') as wa_jid, 
    COALESCE(picture, '') as picture, 
    points, 
    streak, 
    level, 
    xp, 
    daily_goal, 
    last_completed_at, 
    created_at, 
    streak_freezes 
FROM users;
