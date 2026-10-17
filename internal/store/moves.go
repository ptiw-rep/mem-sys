package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type MoveRequest struct {
	ID                 string     `json:"id"`
	FactID             string     `json:"fact_id"`
	FromModuleID       string     `json:"from_module_id"`
	ToModuleID         string     `json:"to_module_id"`
	Status             string     `json:"status"`
	Reason             string     `json:"reason,omitempty"`
	InitiatedBy        string     `json:"initiated_by"`
	SourceApprovedBy   *string    `json:"source_approved_by,omitempty"`
	TargetApprovedBy   *string    `json:"target_approved_by,omitempty"`
	RoleUsedSource     string     `json:"role_used_source,omitempty"`
	RoleUsedTarget     string     `json:"role_used_target,omitempty"`
	AdminOverrideUsed  bool       `json:"admin_override_used"`
	CreatedAt          time.Time  `json:"created_at"`
	ResolvedAt         *string    `json:"resolved_at,omitempty"`
}

func (s *Store) CreateMoveRequest(ctx context.Context, m MoveRequest) error {
	id := m.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.ExecContext(ctx,
		`INSERT INTO move_requests (id, fact_id, from_module_id, to_module_id, status, reason, initiated_by, source_approved_by, target_approved_by, role_used_source, role_used_target, admin_override_used, created_at, resolved_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, m.FactID, m.FromModuleID, m.ToModuleID, m.Status, m.Reason, m.InitiatedBy, m.SourceApprovedBy, m.TargetApprovedBy, m.RoleUsedSource, m.RoleUsedTarget, boolToInt(m.AdminOverrideUsed), time.Now().UTC().Format(time.RFC3339), m.ResolvedAt,
	)
	if err != nil {
		return fmt.Errorf("create move request: %w", err)
	}
	return nil
}

func (s *Store) GetMoveRequest(ctx context.Context, id string) (*MoveRequest, error) {
	row := s.QueryRowContext(ctx, `SELECT id, fact_id, from_module_id, to_module_id, status, reason, initiated_by, source_approved_by, target_approved_by, role_used_source, role_used_target, admin_override_used, created_at, resolved_at FROM move_requests WHERE id = ?`, id)
	return scanMoveRequest(row)
}

func (s *Store) ListMoveRequests(ctx context.Context, moduleID *string, status *string) ([]MoveRequest, error) {
	q := `SELECT id, fact_id, from_module_id, to_module_id, status, reason, initiated_by, source_approved_by, target_approved_by, role_used_source, role_used_target, admin_override_used, created_at, resolved_at FROM move_requests`
	args := []interface{}{}
	conds := []string{}
	if moduleID != nil {
		conds = append(conds, `(from_module_id = ? OR to_module_id = ?)`)
		args = append(args, *moduleID, *moduleID)
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
	var out []MoveRequest
	for rows.Next() {
		var m MoveRequest
		var sab, tab, rus, rut, resolved string
		var aou int
		var createdAt string
		if err := rows.Scan(&m.ID, &m.FactID, &m.FromModuleID, &m.ToModuleID, &m.Status, &m.Reason, &m.InitiatedBy, &sab, &tab, &rus, &rut, &aou, &createdAt, &resolved); err != nil {
			return nil, err
		}
		if sab != "" {
			m.SourceApprovedBy = &sab
		}
		if tab != "" {
			m.TargetApprovedBy = &tab
		}
		m.RoleUsedSource = rus
		m.RoleUsedTarget = rut
		m.AdminOverrideUsed = aou == 1
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			m.CreatedAt = t
		}
		if resolved != "" {
			m.ResolvedAt = &resolved
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) UpdateMoveRequest(ctx context.Context, id, status, sourceApprovedBy, targetApprovedBy, roleUsedSource, roleUsedTarget string, adminOverrideUsed bool, resolvedAt *string) error {
	_, err := s.ExecContext(ctx,
		`UPDATE move_requests SET status = ?, source_approved_by = ?, target_approved_by = ?, role_used_source = ?, role_used_target = ?, admin_override_used = ?, resolved_at = ? WHERE id = ?`,
		status, sourceApprovedBy, targetApprovedBy, roleUsedSource, roleUsedTarget, boolToInt(adminOverrideUsed), resolvedAt, id,
	)
	return err
}

func scanMoveRequest(row *sql.Row) (*MoveRequest, error) {
	var m MoveRequest
	var sab, tab, rus, rut, resolved string
	var aou int
	var createdAt string
	if err := row.Scan(&m.ID, &m.FactID, &m.FromModuleID, &m.ToModuleID, &m.Status, &m.Reason, &m.InitiatedBy, &sab, &tab, &rus, &rut, &aou, &createdAt, &resolved); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if sab != "" {
		m.SourceApprovedBy = &sab
	}
	if tab != "" {
		m.TargetApprovedBy = &tab
	}
	m.RoleUsedSource = rus
	m.RoleUsedTarget = rut
	m.AdminOverrideUsed = aou == 1
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		m.CreatedAt = t
	}
	if resolved != "" {
		m.ResolvedAt = &resolved
	}
	return &m, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
