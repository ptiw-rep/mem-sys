package db

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
	logger *slog.Logger
}

func Open(path string) (*DB, error) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	var conn *sql.DB
	var err error
	if path == ":memory:" {
		conn, err = sql.Open("sqlite3", path+"?mode=memory&cache=shared&_foreign_keys=on")
	} else {
		conn, err = sql.Open("sqlite3", path+"?_foreign_keys=on")
	}
	if err != nil {
		return nil, err
	}

	conn.SetMaxOpenConns(1)

	if err := runMigrations(conn); err != nil {
		conn.Close()
		return nil, err
	}

	return &DB{DB: conn, logger: logger}, nil
}

func runMigrations(conn *sql.DB) error {
	entries, err := os.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join("migrations", e.Name()))
		if err != nil {
			return err
		}
		if _, err := conn.Exec(string(data)); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) Close() error {
	return d.DB.Close()
}
