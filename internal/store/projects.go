package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ProjectPattern struct {
	ProjectID    string   `json:"project_id"`
	PathPatterns []string `json:"path_patterns"`
	UpdatedAt    string   `json:"updated_at"`
}

func (s *Store) CreateProject(ctx context.Context, p Project) (*Project, error) {
	id := p.ID
	if id == "" {
		id = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.ExecContext(ctx,
		`INSERT INTO projects (id, name, description, status, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, p.Name, p.Description, p.Status, p.CreatedBy, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	p.ID = id
	p.CreatedAt, _ = time.Parse(time.RFC3339, now)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, now)
	return &p, nil
}

func (s *Store) GetProject(ctx context.Context, id string) (*Project, error) {
	row := s.QueryRowContext(ctx, `SELECT id, name, description, status, created_by, created_at, updated_at FROM projects WHERE id = ?`, id)
	return scanProject(row)
}

func (s *Store) GetProjectByName(ctx context.Context, name string) (*Project, error) {
	row := s.QueryRowContext(ctx, `SELECT id, name, description, status, created_by, created_at, updated_at FROM projects WHERE name = ?`, name)
	return scanProject(row)
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, name, description, status, created_by, created_at, updated_at FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		var createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.CreatedBy, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
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

func (s *Store) PatchProject(ctx context.Context, id string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}
	set := []string{}
	args := []interface{}{}
	for k, v := range updates {
		set = append(set, fmt.Sprintf("%s = ?", k))
		args = append(args, v)
	}
	set = append(set, "updated_at = ?")
	args = append(args, time.Now().UTC().Format(time.RFC3339))
	args = append(args, id)
	q := `UPDATE projects SET ` + join(set, ", ") + ` WHERE id = ?`
	_, err := s.ExecContext(ctx, q, args...)
	return err
}

func scanProject(row *sql.Row) (*Project, error) {
	var p Project
	var createdAt, updatedAt string
	if err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.CreatedBy, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		p.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		p.UpdatedAt = t
	}
	return &p, nil
}

// ProjectPatterns manages per-project path pattern allowlists stored in the DB.
func (s *Store) SetProjectPatterns(ctx context.Context, projectID string, patterns []string) error {
	pp, err := json.Marshal(patterns)
	if err != nil {
		return fmt.Errorf("marshal patterns: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.ExecContext(ctx,
		`INSERT OR REPLACE INTO project_patterns (project_id, path_patterns, updated_at) VALUES (?, ?, ?)`,
		projectID, string(pp), now,
	)
	return err
}

func (s *Store) GetProjectPatterns(ctx context.Context, projectID string) ([]string, error) {
	row := s.QueryRowContext(ctx, `SELECT path_patterns FROM project_patterns WHERE project_id = ?`, projectID)
	var pp string
	if err := row.Scan(&pp); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var patterns []string
	if err := json.Unmarshal([]byte(pp), &patterns); err != nil {
		return nil, fmt.Errorf("unmarshal patterns: %w", err)
	}
	return patterns, nil
}
