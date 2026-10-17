package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type ProposalIntent string

const (
	IntentNewFact       ProposalIntent = "new_fact"
	IntentAmendFact     ProposalIntent = "amend_fact"
	IntentAnchorUpdate  ProposalIntent = "anchor_update"
	IntentMoveFact      ProposalIntent = "move_fact"
	IntentClarification ProposalIntent = "clarification"
	IntentQuestion      ProposalIntent = "question"
)

type Proposal struct {
	ID                 string     `json:"id"`
	IdempotencyKey     string     `json:"idempotency_key"`
	SubmitterID        string     `json:"submitter_id"`
	Source             string     `json:"source"`
	RawPayload         string     `json:"raw_payload"`
	NormalizedText     string     `json:"normalized_text"`
	Intent             ProposalIntent `json:"intent"`
	LinkedFactID       *string    `json:"linked_fact_id,omitempty"`
	TargetModuleID     *string    `json:"target_module_id,omitempty"`
	Status             string     `json:"status"`
	MappingConfidence  float64    `json:"mapping_confidence"`
	MappingExplanation string     `json:"mapping_explanation,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (s *Store) CreateProposal(ctx context.Context, p Proposal) error {
	id := p.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.ExecContext(ctx,
		`INSERT INTO proposals (id, idempotency_key, submitter_id, source, raw_payload, normalized_text, intent, linked_fact_id, target_module_id, status, mapping_confidence, mapping_explanation, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, p.IdempotencyKey, p.SubmitterID, p.Source, p.RawPayload, p.NormalizedText, p.Intent, p.LinkedFactID, p.TargetModuleID, p.Status, p.MappingConfidence, p.MappingExplanation, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create proposal: %w", err)
	}
	return nil
}

func (s *Store) GetProposal(ctx context.Context, id string) (*Proposal, error) {
	row := s.QueryRowContext(ctx, `SELECT id, idempotency_key, submitter_id, source, raw_payload, normalized_text, intent, linked_fact_id, target_module_id, status, mapping_confidence, mapping_explanation, created_at, updated_at FROM proposals WHERE id = ?`, id)
	return scanProposal(row)
}

func (s *Store) GetProposalByIdempotencyKey(ctx context.Context, key string) (*Proposal, error) {
	row := s.QueryRowContext(ctx, `SELECT id, idempotency_key, submitter_id, source, raw_payload, normalized_text, intent, linked_fact_id, target_module_id, status, mapping_confidence, mapping_explanation, created_at, updated_at FROM proposals WHERE idempotency_key = ?`, key)
	return scanProposal(row)
}

func (s *Store) ListProposals(ctx context.Context, moduleID *string, status *string) ([]Proposal, error) {
	q := `SELECT id, idempotency_key, submitter_id, source, raw_payload, normalized_text, intent, linked_fact_id, target_module_id, status, mapping_confidence, mapping_explanation, created_at, updated_at FROM proposals`
	args := []interface{}{}
	conds := []string{}
	if moduleID != nil {
		conds = append(conds, `target_module_id = ?`)
		args = append(args, *moduleID)
	}
	if status != nil {
		conds = append(conds, `status = ?`)
		args = append(args, *status)
	}
	if len(conds) > 0 {
		q += ` WHERE ` + join(conds, ` AND `)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Proposal
	for rows.Next() {
		var p Proposal
		var linkedFA, targetM *string
		var createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.IdempotencyKey, &p.SubmitterID, &p.Source, &p.RawPayload, &p.NormalizedText, &p.Intent, &linkedFA, &targetM, &p.Status, &p.MappingConfidence, &p.MappingExplanation, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.LinkedFactID = linkedFA
		p.TargetModuleID = targetM
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			p.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			p.UpdatedAt = t
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) UpdateProposalStatus(ctx context.Context, id, status string, confidence float64, explanation string) error {
	_, err := s.ExecContext(ctx,
		`UPDATE proposals SET status = ?, mapping_confidence = ?, mapping_explanation = ?, updated_at = ? WHERE id = ?`,
		status, confidence, explanation, time.Now().UTC().Format(time.RFC3339), id,
	)
	return err
}

func scanProposal(row *sql.Row) (*Proposal, error) {
	var p Proposal
	var linkedFA, targetM *string
	var createdAt, updatedAt string
	if err := row.Scan(&p.ID, &p.IdempotencyKey, &p.SubmitterID, &p.Source, &p.RawPayload, &p.NormalizedText, &p.Intent, &linkedFA, &targetM, &p.Status, &p.MappingConfidence, &p.MappingExplanation, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	p.LinkedFactID = linkedFA
	p.TargetModuleID = targetM
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		p.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		p.UpdatedAt = t
	}
	return &p, nil
}
