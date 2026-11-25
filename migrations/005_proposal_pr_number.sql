-- Migration 005: Add pr_number to proposals for filtering
PRAGMA foreign_keys = ON;

ALTER TABLE proposals ADD COLUMN pr_number INTEGER;

CREATE INDEX IF NOT EXISTS idx_proposals_pr_number ON proposals(pr_number);
