-- Migration 004: Multi-project support
PRAGMA foreign_keys = ON;

-- Projects table
CREATE TABLE IF NOT EXISTS projects (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    description   TEXT,
    status        TEXT NOT NULL DEFAULT 'active',
    created_by    TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

-- Per-project path pattern allowlists (replaces the single global project_pattern.json)
CREATE TABLE IF NOT EXISTS project_patterns (
    project_id   TEXT NOT NULL,
    path_patterns TEXT NOT NULL DEFAULT '[]',
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (project_id),
    FOREIGN KEY (project_id) REFERENCES projects(id)
);

-- Add project_id to modules
ALTER TABLE modules ADD COLUMN project_id TEXT;

-- Create a default project and backfill existing modules
INSERT OR IGNORE INTO projects (id, name, description, status, created_by, created_at, updated_at)
VALUES ('project-default', 'Default Project', 'Default project for existing modules', 'active', 'system', datetime('now'), datetime('now'));

UPDATE modules SET project_id = 'project-default' WHERE project_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_modules_project ON modules(project_id);
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);
