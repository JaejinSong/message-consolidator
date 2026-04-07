-- Migration: Refactor is_internal to contact_type
-- Why: Supports granular categorization ('none', 'internal', 'partner', 'customer') instead of binary flag.

-- 1. Add contact_type column with default 'none'
ALTER TABLE contacts ADD COLUMN contact_type TEXT DEFAULT 'none';

-- 2. Migrate existing is_internal data if any (1 -> internal, 0 -> none)
UPDATE contacts SET contact_type = 'internal' WHERE is_internal = 1;
UPDATE contacts SET contact_type = 'none' WHERE is_internal = 0;

-- 3. Drop legacy is_internal column
ALTER TABLE contacts DROP COLUMN is_internal;
