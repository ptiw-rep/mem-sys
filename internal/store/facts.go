package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Fact struct {
	ID                string     `json:"id"`
	Title             string     `json:"title"`
	Kind              string     `json:"kind"`
	Status            string     `json:"status"`
	CurrentRevisionID *string    `json:"current_revision_id,omitempty"`
	OriginProposalID  *string    `json:"origin_proposal_id,omitempty"`
	CreatedBy         string     `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type FactRevision struct {
	ID               string     `json:"id"`
	FactID           string     `json:"fact_id"`
	Version          int        `json:"version"`
	Title            string     `json:"title"`
	Body             string     `json:"body"`
	StructuredPayload string    `json:"structured_payload,omitempty"`
	AuthorID         string     `json:"author_id"`
	ApprovedBy       *string    `json:"approved_by,omitempty"`
	SourceProposalID *string    `json:"source_proposal_id,omitempty"`
	ChangeReason     string     `json:"change_reason,omitempty"`
	ContentHash      string     `json:"content_hash,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type FactPlacement struct {
	ID            string     `json:"id"`
	FactID        string     `json:"fact_id"`
	ModuleID      string     `json:"module_id"`
	PlacementType string     `json:"placement_type"`
	Status        string     `json:"status"`
	ProposedBy    *string    `json:"proposed_by,omitempty"`
	ApprovedBy    *string    `json:"approved_by,omitempty"`
	Reason        string     `json:"reason,omitempty"`
	ValidFrom     *string    `json:"valid_from,omitempty"`
	ValidTo       *string    `json:"valid_to,omitempty"`
}

func (s *Store) CreateFact(ctx context.Context, f Fact) error {
	id := f.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.ExecContext(ctx,
		`INSERT INTO facts (id, title, kind, status, current_revision_id, origin_proposal_id, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, f.Title, f.Kind, f.Status, f.CurrentRevisionID, f.OriginProposalID, f.CreatedBy, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create fact: %w", err)
	}
	return nil
}

func (s *Store) GetFact(ctx context.Context, id string) (*Fact, error) {
	row := s.QueryRowContext(ctx, `SELECT id, title, kind, status, current_revision_id, origin_proposal_id, created_by, created_at, updated_at FROM facts WHERE id = ?`, id)
	return scanFact(row)
}

func (s *Store) ListFacts(ctx context.Context, moduleID *string, status *string) ([]Fact, error) {
	q := `SELECT f.id, f.title, f.kind, f.status, f.current_revision_id, f.origin_proposal_id, f.created_by, f.created_at, f.updated_at FROM facts f`
	args := []interface{}{}
	conds := []string{}
	if moduleID != nil {
		conds = append(conds, `EXISTS (SELECT 1 FROM fact_placements p WHERE p.fact_id = f.id AND p.module_id = ? AND p.status = 'active')`)
		args = append(args, *moduleID)
	}
	if status != nil {
		conds = append(conds, `f.status = ?`)
		args = append(args, *status)
	}
	if len(conds) > 0 {
		q += ` WHERE ` + join(conds, ` AND `)
	}
	q += ` ORDER BY f.created_at DESC`
	rows, err := s.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Fact
	for rows.Next() {
		var f Fact
		var cr, op string
		var createdAt, updatedAt string
		if err := rows.Scan(&f.ID, &f.Title, &f.Kind, &f.Status, &cr, &op, &f.CreatedBy, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if cr != "" {
			f.CurrentRevisionID = &cr
		}
		if op != "" {
			f.OriginProposalID = &op
		}
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			f.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			f.UpdatedAt = t
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) UpdateFactStatus(ctx context.Context, id, status string) error {
	_, err := s.ExecContext(ctx, `UPDATE facts SET status = ?, updated_at = ? WHERE id = ?`, status, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *Store) SetFactCurrentRevision(ctx context.Context, factID, revisionID string) error {
	_, err := s.ExecContext(ctx, `UPDATE facts SET current_revision_id = ?, updated_at = ? WHERE id = ?`, revisionID, time.Now().UTC().Format(time.RFC3339), factID)
	return err
}

func scanFact(row *sql.Row) (*Fact, error) {
	var f Fact
	var cr, op string
	var createdAt, updatedAt string
	if err := row.Scan(&f.ID, &f.Title, &f.Kind, &f.Status, &cr, &op, &f.CreatedBy, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if cr != "" {
		f.CurrentRevisionID = &cr
	}
	if op != "" {
		f.OriginProposalID = &op
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		f.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		f.UpdatedAt = t
	}
	return &f, nil
}

func (s *Store) CreateFactRevision(ctx context.Context, r FactRevision) error {
	id := r.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.ExecContext(ctx,
		`INSERT INTO fact_revisions (id, fact_id, version, title, body, structured_payload, author_id, approved_by, source_proposal_id, change_reason, content_hash, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, r.FactID, r.Version, r.Title, r.Body, r.StructuredPayload, r.AuthorID, r.ApprovedBy, r.SourceProposalID, r.ChangeReason, r.ContentHash, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *Store) GetFactRevision(ctx context.Context, id string) (*FactRevision, error) {
	row := s.QueryRowContext(ctx, `SELECT id, fact_id, version, title, body, structured_payload, author_id, approved_by, source_proposal_id, change_reason, content_hash, created_at FROM fact_revisions WHERE id = ?`, id)
	return scanFactRevision(row)
}

func (s *Store) ListFactRevisions(ctx context.Context, factID string) ([]FactRevision, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, fact_id, version, title, body, structured_payload, author_id, approved_by, source_proposal_id, change_reason, content_hash, created_at FROM fact_revisions WHERE fact_id = ? ORDER BY version`, factID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FactRevision
	for rows.Next() {
		var r FactRevision
		var ap, sp string
		var createdAt string
		if err := rows.Scan(&r.ID, &r.FactID, &r.Version, &r.Title, &r.Body, &r.StructuredPayload, &r.AuthorID, &ap, &sp, &r.ChangeReason, &r.ContentHash, &createdAt); err != nil {
			return nil, err
		}
		if ap != "" {
			r.ApprovedBy = &ap
		}
		if sp != "" {
			r.SourceProposalID = &sp
		}
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			r.CreatedAt = t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanFactRevision(row *sql.Row) (*FactRevision, error) {
	var r FactRevision
	var ap, sp string
	var createdAt string
	if err := row.Scan(&r.ID, &r.FactID, &r.Version, &r.Title, &r.Body, &r.StructuredPayload, &r.AuthorID, &ap, &sp, &r.ChangeReason, &r.ContentHash, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if ap != "" {
		r.ApprovedBy = &ap
	}
	if sp != "" {
		r.SourceProposalID = &sp
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		r.CreatedAt = t
	}
	return &r, nil
}

func (s *Store) CreateFactPlacement(ctx context.Context, p FactPlacement) error {
	id := p.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.ExecContext(ctx,
		`INSERT INTO fact_placements (id, fact_id, module_id, placement_type, status, proposed_by, approved_by, reason, valid_from, valid_to) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, p.FactID, p.ModuleID, p.PlacementType, p.Status, p.ProposedBy, p.ApprovedBy, p.Reason, p.ValidFrom, p.ValidTo,
	)
	return err
}

func (s *Store) GetFactPlacements(ctx context.Context, factID string) ([]FactPlacement, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, fact_id, module_id, placement_type, status, proposed_by, approved_by, reason, valid_from, valid_to FROM fact_placements WHERE fact_id = ? ORDER BY created_at`, factID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FactPlacement
	for rows.Next() {
		var p FactPlacement
		var pb, ab, rf, vt string
		if err := rows.Scan(&p.ID, &p.FactID, &p.ModuleID, &p.PlacementType, &p.Status, &pb, &ab, &p.Reason, &rf, &vt); err != nil {
			return nil, err
		}
		if pb != "" {
			p.ProposedBy = &pb
		}
		if ab != "" {
			p.ApprovedBy = &ab
		}
		if rf != "" {
			p.ValidFrom = &rf
		}
		if vt != "" {
			p.ValidTo = &vt
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) UpdateFactPlacementStatus(ctx context.Context, placementID, status string, approvedBy *string) error {
	_, err := s.ExecContext(ctx, `UPDATE fact_placements SET status = ?, approved_by = ? WHERE id = ?`, status, approvedBy, placementID)
	return err
}

func (s *Store) GetActivePlacementForFactInModule(ctx context.Context, factID, moduleID string) (*FactPlacement, error) {
	row := s.QueryRowContext(ctx, `SELECT id, fact_id, module_id, placement_type, status, proposed_by, approved_by, reason, valid_from, valid_to FROM fact_placements WHERE fact_id = ? AND module_id = ? AND status = 'active' LIMIT 1`, factID, moduleID)
	return scanFactPlacement(row)
}

func scanFactPlacement(row *sql.Row) (*FactPlacement, error) {
	var p FactPlacement
	var pb, ab, rf, vt string
	if err := row.Scan(&p.ID, &p.FactID, &p.ModuleID, &p.PlacementType, &p.Status, &pb, &ab, &p.Reason, &rf, &vt); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if pb != "" {
		p.ProposedBy = &pb
	}
	if ab != "" {
		p.ApprovedBy = &ab
	}
	if rf != "" {
		p.ValidFrom = &rf
	}
	if vt != "" {
		p.ValidTo = &vt
	}
	return &p, nil
}
