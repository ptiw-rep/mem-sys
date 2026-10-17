-- Migration 001: Core governance MVP schema
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS principals (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    type          TEXT NOT NULL DEFAULT 'human',
    global_role   TEXT NOT NULL DEFAULT 'user',
    created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS modules (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL UNIQUE,
    description       TEXT,
    repo_id           TEXT NOT NULL DEFAULT 'repo-main',
    path_patterns     TEXT NOT NULL DEFAULT '[]',
    exclude_patterns  TEXT NOT NULL DEFAULT '[]',
    status            TEXT NOT NULL DEFAULT 'active',
    created_by        TEXT NOT NULL,
    created_at        TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS module_owners (
    module_id   TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    granted_by  TEXT NOT NULL,
    granted_at  TEXT NOT NULL,
    PRIMARY KEY (module_id, principal_id),
    FOREIGN KEY (module_id) REFERENCES modules(id),
    FOREIGN KEY (principal_id) REFERENCES principals(id)
);

CREATE TABLE IF NOT EXISTS proposals (
    id                TEXT PRIMARY KEY,
    idempotency_key   TEXT UNIQUE,
    submitter_id      TEXT NOT NULL,
    source            TEXT NOT NULL,
    raw_payload       TEXT NOT NULL,
    normalized_text   TEXT,
    intent            TEXT NOT NULL,
    target_module_id  TEXT,
    status            TEXT NOT NULL DEFAULT 'submitted',
    mapping_confidence REAL DEFAULT 0,
    mapping_explanation TEXT,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    FOREIGN KEY (submitter_id) REFERENCES principals(id),
    FOREIGN KEY (target_module_id) REFERENCES modules(id)
);

CREATE TABLE IF NOT EXISTS facts (
    id                TEXT PRIMARY KEY,
    title             TEXT NOT NULL,
    kind              TEXT NOT NULL DEFAULT 'other',
    status            TEXT NOT NULL DEFAULT 'active',
    current_revision_id TEXT,
    origin_proposal_id TEXT,
    created_by        TEXT NOT NULL,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    FOREIGN KEY (created_by) REFERENCES principals(id),
    FOREIGN KEY (origin_proposal_id) REFERENCES proposals(id),
    FOREIGN KEY (current_revision_id) REFERENCES fact_revisions(id)
);

CREATE TABLE IF NOT EXISTS fact_revisions (
    id                TEXT PRIMARY KEY,
    fact_id           TEXT NOT NULL,
    version           INTEGER NOT NULL,
    title             TEXT NOT NULL,
    body              TEXT NOT NULL,
    structured_payload TEXT,
    author_id         TEXT NOT NULL,
    approved_by       TEXT,
    source_proposal_id TEXT,
    change_reason     TEXT,
    content_hash      TEXT,
    created_at        TEXT NOT NULL,
    FOREIGN KEY (fact_id) REFERENCES facts(id),
    FOREIGN KEY (author_id) REFERENCES principals(id),
    FOREIGN KEY (source_proposal_id) REFERENCES proposals(id)
);

CREATE TABLE IF NOT EXISTS fact_placements (
    id               TEXT PRIMARY KEY,
    fact_id          TEXT NOT NULL,
    module_id        TEXT NOT NULL,
    placement_type   TEXT NOT NULL DEFAULT 'primary',
    status           TEXT NOT NULL DEFAULT 'active',
    proposed_by      TEXT,
    approved_by      TEXT,
    reason           TEXT,
    valid_from       TEXT,
    valid_to         TEXT,
    FOREIGN KEY (fact_id) REFERENCES facts(id),
    FOREIGN KEY (module_id) REFERENCES modules(id),
    FOREIGN KEY (proposed_by) REFERENCES principals(id),
    FOREIGN KEY (approved_by) REFERENCES principals(id)
);

CREATE TABLE IF NOT EXISTS move_requests (
    id                  TEXT PRIMARY KEY,
    fact_id             TEXT NOT NULL,
    from_module_id      TEXT NOT NULL,
    to_module_id        TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'proposed',
    reason              TEXT,
    initiated_by        TEXT NOT NULL,
    source_approved_by  TEXT,
    target_approved_by  TEXT,
    role_used_source    TEXT,
    role_used_target    TEXT,
    admin_override_used INTEGER DEFAULT 0,
    created_at          TEXT NOT NULL,
    resolved_at         TEXT,
    FOREIGN KEY (fact_id) REFERENCES facts(id),
    FOREIGN KEY (from_module_id) REFERENCES modules(id),
    FOREIGN KEY (to_module_id) REFERENCES modules(id),
    FOREIGN KEY (initiated_by) REFERENCES principals(id),
    FOREIGN KEY (source_approved_by) REFERENCES principals(id),
    FOREIGN KEY (target_approved_by) REFERENCES principals(id)
);

CREATE TABLE IF NOT EXISTS facts_anchors (
    id                  TEXT PRIMARY KEY,
    fact_id             TEXT NOT NULL,
    revision_id         TEXT,
    repo_id             TEXT NOT NULL DEFAULT 'repo-main',
    anchor_type         TEXT NOT NULL DEFAULT 'path',
    path                TEXT,
    symbol_name         TEXT,
    qualified_name      TEXT,
    signature_hash      TEXT,
    start_line          INTEGER,
    end_line            INTEGER,
    context_hash        TEXT,
    tree_sitter_query   TEXT,
    embedding_ref       TEXT,
    confidence          REAL DEFAULT 0,
    status              TEXT NOT NULL DEFAULT 'valid',
    last_verified_commit TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    FOREIGN KEY (fact_id) REFERENCES facts(id),
    FOREIGN KEY (revision_id) REFERENCES fact_revisions(id)
);

CREATE TABLE IF NOT EXISTS event_log (
    id                  TEXT PRIMARY KEY,
    aggregate_type      TEXT NOT NULL,
    aggregate_id        TEXT NOT NULL,
    actor_id            TEXT NOT NULL,
    actor_global_role   TEXT NOT NULL,
    role_used           TEXT NOT NULL,
    admin_override_used INTEGER DEFAULT 0,
    action              TEXT NOT NULL,
    payload             TEXT,
    correlation_id      TEXT,
    occurred_at         TEXT NOT NULL,
    FOREIGN KEY (actor_id) REFERENCES principals(id)
);

CREATE INDEX IF NOT EXISTS idx_proposals_status ON proposals(status);
CREATE INDEX IF NOT EXISTS idx_proposals_target_module ON proposals(target_module_id);
CREATE INDEX IF NOT EXISTS idx_proposals_submitter ON proposals(submitter_id);
CREATE INDEX IF NOT EXISTS idx_facts_status ON facts(status);
CREATE INDEX IF NOT EXISTS idx_fact_placements_fact ON fact_placements(fact_id);
CREATE INDEX IF NOT EXISTS idx_fact_placements_module ON fact_placements(module_id);
CREATE INDEX IF NOT EXISTS idx_move_requests_fact ON move_requests(fact_id);
CREATE INDEX IF NOT EXISTS idx_move_requests_status ON move_requests(status);
CREATE INDEX IF NOT EXISTS idx_event_log_aggregate ON event_log(aggregate_type, aggregate_id);
