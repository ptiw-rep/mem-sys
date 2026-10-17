package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Principal struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	GlobalRole  string    `json:"global_role"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Store) CreatePrincipal(ctx context.Context, p Principal) error {
	id := p.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.ExecContext(ctx,
		`INSERT INTO principals (id, name, type, global_role, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, p.Name, p.Type, p.GlobalRole, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create principal: %w", err)
	}
	return nil
}

func (s *Store) GetPrincipal(ctx context.Context, id string) (*Principal, error) {
	row := s.QueryRowContext(ctx, `SELECT id, name, type, global_role, created_at FROM principals WHERE id = ?`, id)
	return scanPrincipal(row)
}

func (s *Store) ListPrincipals(ctx context.Context) ([]Principal, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, name, type, global_role, created_at FROM principals ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Principal
	for rows.Next() {
		var p Principal
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.GlobalRole, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetPrincipalByName(ctx context.Context, name string) (*Principal, error) {
	row := s.QueryRowContext(ctx, `SELECT id, name, type, global_role, created_at FROM principals WHERE name = ?`, name)
	return scanPrincipal(row)
}

func scanPrincipal(row *sql.Row) (*Principal, error) {
	var p Principal
	var createdAt string
	if err := row.Scan(&p.ID, &p.Name, &p.Type, &p.GlobalRole, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		p.CreatedAt = t
	}
	return &p, nil
}

type ModuleOwnerGrant struct {
	ModuleID  string `json:"module_id"`
	PrincipalID string `json:"principal_id"`
	GrantedBy string `json:"granted_by"`
	GrantedAt string `json:"granted_at"`
}

func (s *Store) GrantModuleOwner(ctx context.Context, g ModuleOwnerGrant) error {
	_, err := s.ExecContext(ctx,
		`INSERT OR IGNORE INTO module_owners (module_id, principal_id, granted_by, granted_at) VALUES (?, ?, ?, ?)`,
		g.ModuleID, g.PrincipalID, g.GrantedBy, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *Store) RevokeModuleOwner(ctx context.Context, moduleID, principalID string) error {
	_, err := s.ExecContext(ctx, `DELETE FROM module_owners WHERE module_id = ? AND principal_id = ?`, moduleID, principalID)
	return err
}

func (s *Store) ListModuleOwners(ctx context.Context, moduleID string) ([]Principal, error) {
	rows, err := s.QueryContext(ctx,
		`SELECT p.id, p.name, p.type, p.global_role, p.created_at FROM principals p JOIN module_owners mo ON mo.principal_id = p.id WHERE mo.module_id = ? ORDER BY p.name`,
		moduleID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Principal
	for rows.Next() {
		var p Principal
		var createdAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.GlobalRole, &createdAt); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			p.CreatedAt = t
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ListModulesOwnedByPrincipal(ctx context.Context, principalID string) ([]string, error) {
	rows, err := s.QueryContext(ctx, `SELECT module_id FROM module_owners WHERE principal_id = ?`, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) IsModuleOwner(ctx context.Context, moduleID, principalID string) (bool, error) {
	var count int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM module_owners WHERE module_id = ? AND principal_id = ?`, moduleID, principalID).Scan(&count)
	return count > 0, err
}

func (s *Store) ModuleOwnersJSON(ctx context.Context, moduleID string) ([]byte, error) {
	owners, err := s.ListModuleOwners(ctx, moduleID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(owners)
}
