-- Migration 003: Add updated_at to modules table
PRAGMA foreign_keys = ON;

ALTER TABLE modules ADD COLUMN updated_at TEXT;

-- Backfill existing rows with their created_at value
UPDATE modules SET updated_at = created_at WHERE updated_at IS NULL;
