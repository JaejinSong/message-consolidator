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
    deadline TEXT
);

-- name: CreateTaskTranslationsTable :exec
CREATE TABLE IF NOT EXISTS task_translations (
    message_id INTEGER REFERENCES messages(id) ON DELETE CASCADE,
    language TEXT NOT NULL,
    translated_text TEXT NOT NULL,
    PRIMARY KEY (message_id, language)
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
    user_email VARCHAR(255) NOT NULL,
    rep_name VARCHAR(255) NOT NULL,
    aliases TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_email, rep_name)
);

-- name: CreateMessagesView :exec
DROP VIEW IF EXISTS v_messages;
CREATE VIEW v_messages AS
SELECT 
    id, 
    user_email, 
    source, 
    COALESCE(room, '') as room, 
    task, 
    requester, 
    COALESCE(assignee, '') as assignee,
    COALESCE(assigned_at, created_at) as assigned_at,
    link, 
    source_ts, 
    COALESCE(original_text, '') as original_text, 
    done, 
    is_deleted, 
    created_at, 
    completed_at, 
    COALESCE(category, 'todo') as category, 
    COALESCE(deadline, '') as deadline 
FROM messages;

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
