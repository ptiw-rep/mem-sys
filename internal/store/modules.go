package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Module struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	RepoID           string    `json:"repo_id"`
	PathPatterns     []string  `json:"path_patterns"`
	ExcludePatterns  []string  `json:"exclude_patterns"`
	Status           string    `json:"status"`
	CreatedBy        string    `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
	Owners           []Principal `json:"owners,omitempty"`
}

func (s *Store) CreateModule(ctx context.Context, m Module) error {
	id := m.ID
	if id == "" {
		id = uuid.NewString()
	}
	pp, _ := json.Marshal(m.PathPatterns)
	ep, _ := json.Marshal(m.ExcludePatterns)
	_, err := s.ExecContext(ctx,
		`INSERT INTO modules (id, name, description, repo_id, path_patterns, exclude_patterns, status, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, m.Name, m.Description, m.RepoID, string(pp), string(ep), m.Status, m.CreatedBy, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create module: %w", err)
	}
	return nil
}

func (s *Store) GetModule(ctx context.Context, id string) (*Module, error) {
	row := s.QueryRowContext(ctx, `SELECT id, name, description, repo_id, path_patterns, exclude_patterns, status, created_by, created_at FROM modules WHERE id = ?`, id)
	return scanModule(row, s)
}

func (s *Store) GetModuleByName(ctx context.Context, name string) (*Module, error) {
	row := s.QueryRowContext(ctx, `SELECT id, name, description, repo_id, path_patterns, exclude_patterns, status, created_by, created_at FROM modules WHERE name = ?`, name)
	return scanModule(row, s)
}

func (s *Store) ListModules(ctx context.Context) ([]Module, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, name, description, repo_id, path_patterns, exclude_patterns, status, created_by, created_at FROM modules ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Module
	for rows.Next() {
		var m Module
		var pp, ep string
		var createdAt string
		if err := rows.Scan(&m.ID, &m.Name, &m.Description, &m.RepoID, &pp, &ep, &m.Status, &m.CreatedBy, &createdAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(pp), &m.PathPatterns)
		_ = json.Unmarshal([]byte(ep), &m.ExcludePatterns)
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			m.CreatedAt = t
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) PatchModule(ctx context.Context, id string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}
	set := []string{}
	args := []interface{}{}
	i := 1
	for k, v := range updates {
		set = append(set, fmt.Sprintf("%s = ?", k))
		args = append(args, v)
		i++
	}
	set = append(set, "updated_at = ?")
	args = append(args, time.Now().UTC().Format(time.RFC3339))
	args = append(args, id)
	q := `UPDATE modules SET ` + join(set, ", ") + ` WHERE id = ?`
	_, err := s.ExecContext(ctx, q, args...)
	return err
}

func scanModule(row *sql.Row, s *Store) (*Module, error) {
	var m Module
	var pp, ep string
	var createdAt string
	if err := row.Scan(&m.ID, &m.Name, &m.Description, &m.RepoID, &pp, &ep, &m.Status, &m.CreatedBy, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(pp), &m.PathPatterns)
	_ = json.Unmarshal([]byte(ep), &m.ExcludePatterns)
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		m.CreatedAt = t
	}
	owners, err := s.ListModuleOwners(context.Background(), m.ID)
	if err == nil {
		m.Owners = owners
	}
	return &m, nil
}

func join(ss []string, sep string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
