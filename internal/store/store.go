// Package store persists tracked findings between runs. A JSON store is the
// default; a stateless store discards state so each run is independent.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/greadee/review-engine/internal/findings"
)

// State is the persisted tracking state.
type State struct {
	RunID    string             `json:"runId"`
	Findings []findings.Finding `json:"findings"`
}

// Store loads and saves tracking state.
type Store interface {
	Load(ctx context.Context) (State, error)
	Save(ctx context.Context, s State) error
}

// JSON is a file-backed store rooted at Dir.
type JSON struct {
	Dir string
}

// NewJSON returns a JSON store in dir.
func NewJSON(dir string) JSON { return JSON{Dir: dir} }

func (j JSON) path() string { return filepath.Join(j.Dir, "state.json") }

// Load reads state, returning an empty state when no file exists.
func (j JSON) Load(_ context.Context) (State, error) {
	data, err := os.ReadFile(j.path())
	if os.IsNotExist(err) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("store: read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("store: parse state: %w", err)
	}
	return s, nil
}

// Save writes state atomically.
func (j JSON) Save(_ context.Context, s State) error {
	if err := os.MkdirAll(j.Dir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("store: encode state: %w", err)
	}
	tmp := j.path() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("store: write state: %w", err)
	}
	if err := os.Rename(tmp, j.path()); err != nil {
		return fmt.Errorf("store: commit state: %w", err)
	}
	return nil
}

// Stateless keeps nothing between runs.
type Stateless struct{}

// Load implements Store.
func (Stateless) Load(context.Context) (State, error) { return State{}, nil }

// Save implements Store.
func (Stateless) Save(context.Context, State) error { return nil }
