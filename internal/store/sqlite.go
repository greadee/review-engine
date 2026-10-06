package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/greadee/review-engine/internal/findings"
	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// SQLite is a tracking store backed by a SQLite database. It is opt-in; the
// JSON store is the default.
type SQLite struct {
	db *sql.DB
}

// OpenSQLite opens (creating if needed) a SQLite tracking store at path.
func OpenSQLite(path string) (*SQLite, error) {
	if path != ":memory:" {
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("store: create directory: %w", err)
			}
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS findings (
		id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL,
		data TEXT NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: init schema: %w", err)
	}
	return &SQLite{db: db}, nil
}

// Load implements Store.
func (s *SQLite) Load(ctx context.Context) (State, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT run_id, data FROM findings`)
	if err != nil {
		return State{}, fmt.Errorf("store: query: %w", err)
	}
	defer rows.Close()
	var state State
	for rows.Next() {
		var runID, data string
		if err := rows.Scan(&runID, &data); err != nil {
			return State{}, fmt.Errorf("store: scan: %w", err)
		}
		var f findings.Finding
		if err := json.Unmarshal([]byte(data), &f); err != nil {
			return State{}, fmt.Errorf("store: decode finding: %w", err)
		}
		state.RunID = runID
		state.Findings = append(state.Findings, f)
	}
	return state, rows.Err()
}

// Save implements Store. It replaces the tracked set in a transaction.
func (s *SQLite) Save(ctx context.Context, st State) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM findings`); err != nil {
		return fmt.Errorf("store: clear: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO findings (id, run_id, data) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("store: prepare: %w", err)
	}
	defer stmt.Close()
	for _, f := range st.Findings {
		data, err := json.Marshal(f)
		if err != nil {
			return fmt.Errorf("store: encode finding: %w", err)
		}
		if _, err := stmt.ExecContext(ctx, f.ID, st.RunID, string(data)); err != nil {
			return fmt.Errorf("store: insert: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// Close releases the database handle.
func (s *SQLite) Close() error { return s.db.Close() }

// Open selects a tracking backend. When sqlite is false (the default) it
// returns a JSON store rooted at stateDir. The returned close function is a
// no-op for JSON.
func Open(sqlite bool, stateDir, sqlitePath string) (Store, func() error, error) {
	if !sqlite {
		return NewJSON(stateDir), func() error { return nil }, nil
	}
	s, err := OpenSQLite(sqlitePath)
	if err != nil {
		return nil, nil, err
	}
	return s, s.Close, nil
}
