-- Migration 002: Idempotency key registry (additive)
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS idempotency_keys (
    key            TEXT PRIMARY KEY,
    principal_id   TEXT NOT NULL,
    expires_at     TEXT NOT NULL,
    max_uses       INTEGER NOT NULL DEFAULT 1,
    used_count     INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL,
    FOREIGN KEY (principal_id) REFERENCES principals(id)
);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_principal ON idempotency_keys(principal_id);
CREATE INDEX IF NOT EXISTS idx_idempotency_keys_expires_at ON idempotency_keys(expires_at);
