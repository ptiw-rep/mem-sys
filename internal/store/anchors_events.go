package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type FactAnchor struct {
	ID                 string     `json:"id"`
	FactID             string     `json:"fact_id"`
	RevisionID         *string    `json:"revision_id,omitempty"`
	RepoID             string     `json:"repo_id"`
	AnchorType         string     `json:"anchor_type"`
	Path               string     `json:"path,omitempty"`
	SymbolName         string     `json:"symbol_name,omitempty"`
	QualifiedName      string     `json:"qualified_name,omitempty"`
	SignatureHash      string     `json:"signature_hash,omitempty"`
	StartLine          *int       `json:"start_line,omitempty"`
	EndLine            *int       `json:"end_line,omitempty"`
	ContextHash        string     `json:"context_hash,omitempty"`
	TreeSitterQuery    string     `json:"tree_sitter_query,omitempty"`
	EmbeddingRef       string     `json:"embedding_ref,omitempty"`
	Confidence         float64    `json:"confidence"`
	Status             string     `json:"status"`
	LastVerifiedCommit string     `json:"last_verified_commit,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (s *Store) CreateFactAnchor(ctx context.Context, a FactAnchor) error {
	id := a.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.ExecContext(ctx,
		`INSERT INTO facts_anchors (id, fact_id, revision_id, repo_id, anchor_type, path, symbol_name, qualified_name, signature_hash, start_line, end_line, context_hash, tree_sitter_query, embedding_ref, confidence, status, last_verified_commit, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, a.FactID, a.RevisionID, a.RepoID, a.AnchorType, a.Path, a.SymbolName, a.QualifiedName, a.SignatureHash, a.StartLine, a.EndLine, a.ContextHash, a.TreeSitterQuery, a.EmbeddingRef, a.Confidence, a.Status, a.LastVerifiedCommit, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *Store) ListFactAnchors(ctx context.Context, factID string) ([]FactAnchor, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, fact_id, revision_id, repo_id, anchor_type, path, symbol_name, qualified_name, signature_hash, start_line, end_line, context_hash, tree_sitter_query, embedding_ref, confidence, status, last_verified_commit, created_at, updated_at FROM facts_anchors WHERE fact_id = ? ORDER BY created_at`, factID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FactAnchor
	for rows.Next() {
		var a FactAnchor
		var rid, repoID, path, sym, qname, sig, ch, tsq, er, lvc string
		var sl, el *int
		var conf float64
		var createdAt, updatedAt string
		if err := rows.Scan(&a.ID, &a.FactID, &rid, &repoID, &a.AnchorType, &path, &sym, &qname, &sig, &sl, &el, &ch, &tsq, &er, &conf, &a.Status, &lvc, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if rid != "" {
			a.RevisionID = &rid
		}
		a.RepoID = repoID
		a.Path = path
		a.SymbolName = sym
		a.QualifiedName = qname
		a.SignatureHash = sig
		a.StartLine = sl
		a.EndLine = el
		a.ContextHash = ch
		a.TreeSitterQuery = tsq
		a.EmbeddingRef = er
		a.Confidence = conf
		if lvc != "" {
			a.LastVerifiedCommit = lvc
		}
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			a.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			a.UpdatedAt = t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type Event struct {
	ID                  string `json:"id"`
	AggregateType       string `json:"aggregate_type"`
	AggregateID         string `json:"aggregate_id"`
	ActorID             string `json:"actor_id"`
	ActorGlobalRole     string `json:"actor_global_role"`
	RoleUsed            string `json:"role_used"`
	AdminOverrideUsed   bool   `json:"admin_override_used"`
	Action              string `json:"action"`
	Payload             string `json:"payload,omitempty"`
	CorrelationID       string `json:"correlation_id,omitempty"`
	OccurredAt          string `json:"occurred_at"`
}

func (s *Store) AppendEvent(ctx context.Context, e Event) error {
	id := e.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.ExecContext(ctx,
		`INSERT INTO event_log (id, aggregate_type, aggregate_id, actor_id, actor_global_role, role_used, admin_override_used, action, payload, correlation_id, occurred_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, e.AggregateType, e.AggregateID, e.ActorID, e.ActorGlobalRole, e.RoleUsed, boolToInt(e.AdminOverrideUsed), e.Action, e.Payload, e.CorrelationID, e.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *Store) ListEvents(ctx context.Context, aggregateType, aggregateID string) ([]Event, error) {
	var rows *sql.Rows
	var err error
	if aggregateID != "" {
		rows, err = s.QueryContext(ctx, `SELECT id, aggregate_type, aggregate_id, actor_id, actor_global_role, role_used, admin_override_used, action, payload, correlation_id, occurred_at FROM event_log WHERE aggregate_type = ? AND aggregate_id = ? ORDER BY occurred_at`, aggregateType, aggregateID)
	} else {
		rows, err = s.QueryContext(ctx, `SELECT id, aggregate_type, aggregate_id, actor_id, actor_global_role, role_used, admin_override_used, action, payload, correlation_id, occurred_at FROM event_log ORDER BY occurred_at DESC LIMIT 200`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var aou int
		if err := rows.Scan(&e.ID, &e.AggregateType, &e.AggregateID, &e.ActorID, &e.ActorGlobalRole, &e.RoleUsed, &aou, &e.Action, &e.Payload, &e.CorrelationID, &e.OccurredAt); err != nil {
			return nil, err
		}
		e.AdminOverrideUsed = aou == 1
		out = append(out, e)
	}
	return out, rows.Err()
}
