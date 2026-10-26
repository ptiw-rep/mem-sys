package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type IdempotencyKey struct {
	Key        string     `json:"key"`
	PrincipalID string    `json:"principal_id"`
	ExpiresAt  time.Time  `json:"expires_at"`
	MaxUses    int        `json:"max_uses"`
	UsedCount  int        `json:"used_count"`
	CreatedAt  time.Time  `json:"created_at"`
}

type CreateIdempotencyKeyInput struct {
	PrincipalID string        `json:"principal_id"`
	MaxUses     int           `json:"max_uses"`
	TTL         time.Duration `json:"ttl"`
}

func (s *Store) CreateIdempotencyKey(ctx context.Context, in CreateIdempotencyKeyInput) (*IdempotencyKey, error) {
	// Validate principal exists
	var count int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM principals WHERE id = ?`, in.PrincipalID).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("validate principal: %w", err)
	}
	if count == 0 {
		return nil, fmt.Errorf("principal %s not found", in.PrincipalID)
	}

	key := uuid.NewString()
	maxUses := in.MaxUses
	if maxUses <= 0 {
		maxUses = 1
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)

	_, err = s.ExecContext(ctx,
		`INSERT INTO idempotency_keys (key, principal_id, expires_at, max_uses, used_count, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		key, in.PrincipalID, expiresAt.Format(time.RFC3339), maxUses, 0, now.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("create idempotency key: %w", err)
	}

	return &IdempotencyKey{
		Key:         key,
		PrincipalID: in.PrincipalID,
		ExpiresAt:   expiresAt,
		MaxUses:     maxUses,
		UsedCount:   0,
		CreatedAt:   now,
	}, nil
}

func (s *Store) GetIdempotencyKey(ctx context.Context, key string) (*IdempotencyKey, error) {
	row := s.QueryRowContext(ctx, `SELECT key, principal_id, expires_at, max_uses, used_count, created_at FROM idempotency_keys WHERE key = ?`, key)
	return scanIdempotencyKey(row)
}

func (s *Store) ValidateAndConsumeIdempotencyKey(ctx context.Context, key string) (*IdempotencyKey, error) {
	ik, err := s.GetIdempotencyKey(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get idempotency key: %w", err)
	}

	now := time.Now().UTC()
	if now.After(ik.ExpiresAt) {
		return nil, fmt.Errorf("idempotency key expired at %s", ik.ExpiresAt.Format(time.RFC3339))
	}
	if ik.UsedCount >= ik.MaxUses {
		return nil, fmt.Errorf("idempotency key has reached max uses (%d/%d)", ik.UsedCount, ik.MaxUses)
	}

	_, err = s.ExecContext(ctx, `UPDATE idempotency_keys SET used_count = used_count + 1 WHERE key = ?`, key)
	if err != nil {
		return nil, fmt.Errorf("consume idempotency key: %w", err)
	}

	ik.UsedCount++
	return ik, nil
}

func scanIdempotencyKey(row *sql.Row) (*IdempotencyKey, error) {
	var ik IdempotencyKey
	var expiresAt, createdAt string
	if err := row.Scan(&ik.Key, &ik.PrincipalID, &expiresAt, &ik.MaxUses, &ik.UsedCount, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339, expiresAt); err == nil {
		ik.ExpiresAt = t
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		ik.CreatedAt = t
	}
	return &ik, nil
}
