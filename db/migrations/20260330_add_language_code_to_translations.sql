-- PRAGMA setup
PRAGMA foreign_keys=OFF;

BEGIN TRANSACTION;

-- 1. Migrate report_translations
-- Create temporary table with new schema (no language_deprecated, proper UNIQUE constraint)
CREATE TABLE new_report_translations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id INTEGER NOT NULL,
    language_code TEXT NOT NULL,
    summary TEXT NOT NULL,
    FOREIGN KEY (report_id) REFERENCES reports(id) ON DELETE CASCADE,
    UNIQUE(report_id, language_code)
);

-- Copy data with CASE mapping for language_code
-- Handles both 'language' (original) and 'language_deprecated'/'language_code' (intermediate) source columns.
INSERT INTO new_report_translations (report_id, language_code, summary)
SELECT 
    report_id,
    CASE 
        WHEN language = 'Korean' OR language_code = 'Korean' THEN 'ko'
        WHEN language = 'English' OR language_code = 'English' THEN 'en'
        WHEN language = 'Indonesian' OR language_code = 'Indonesian' THEN 'id'
        WHEN language = 'Thai' OR language_code = 'Thai' THEN 'th'
        ELSE COALESCE(language_code, language, 'en') 
    END as language_code,
    summary
FROM report_translations;

-- Drop old table and rename new one
DROP TABLE report_translations;
ALTER TABLE new_report_translations RENAME TO report_translations;

-- Create index for performance
CREATE UNIQUE INDEX IF NOT EXISTS idx_report_translations_id_lang_code ON report_translations (report_id, language_code);


-- 2. Migrate task_translations
-- Create temporary table with new schema (ISO language_code as part of PK, no language_deprecated)
CREATE TABLE new_task_translations (
    message_id INTEGER NOT NULL,
    language_code TEXT NOT NULL,
    translated_text TEXT NOT NULL,
    PRIMARY KEY (message_id, language_code),
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);

-- Copy data with CASE mapping
INSERT INTO new_task_translations (message_id, language_code, translated_text)
SELECT 
    message_id,
    CASE 
        WHEN language = 'Korean' OR language_code = 'Korean' THEN 'ko'
        WHEN language = 'English' OR language_code = 'English' THEN 'en'
        WHEN language = 'Indonesian' OR language_code = 'Indonesian' THEN 'id'
        WHEN language = 'Thai' OR language_code = 'Thai' THEN 'th'
        ELSE COALESCE(language_code, language, 'en')
    END as language_code,
    translated_text
FROM task_translations;

-- Drop old table and rename new one
DROP TABLE task_translations;
ALTER TABLE new_task_translations RENAME TO task_translations;

-- Create index for performance
CREATE INDEX IF NOT EXISTS idx_task_translations_id_lang_code ON task_translations (language_code);

COMMIT;

-- Restore PRAGMA
PRAGMA foreign_keys=ON;
