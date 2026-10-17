package store

import (
	"database/sql"
	"mem-sys/internal/db"
)

type Store struct {
	*db.DB
}

func New(d *db.DB) *Store {
	return &Store{d}
}

func (s *Store) Tx(fn func(*sql.Tx) error) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
