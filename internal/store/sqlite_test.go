package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/greadee/review-engine/internal/findings"
)

func TestSQLiteRoundTrip(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	if st, err := s.Load(ctx); err != nil || len(st.Findings) != 0 {
		t.Fatalf("empty load: %+v %v", st, err)
	}
	want := State{RunID: "r1", Findings: []findings.Finding{{ID: "x", Title: "t", Severity: findings.P1, Classification: findings.Bug}}}
	if err := s.Save(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.RunID != "r1" || len(got.Findings) != 1 || got.Findings[0].ID != "x" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}

	// Save replaces the tracked set.
	if err := s.Save(ctx, State{RunID: "r2", Findings: []findings.Finding{{ID: "y"}}}); err != nil {
		t.Fatal(err)
	}
	got2, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2.Findings) != 1 || got2.Findings[0].ID != "y" {
		t.Fatalf("expected replacement, got %+v", got2.Findings)
	}
}

func TestOpenSelectsBackend(t *testing.T) {
	jsonStore, closeJSON, err := Open(false, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := jsonStore.(JSON); !ok {
		t.Fatalf("expected JSON store, got %T", jsonStore)
	}
	if err := closeJSON(); err != nil {
		t.Fatal(err)
	}

	sqlStore, closeSQL, err := Open(true, t.TempDir(), filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sqlStore.(*SQLite); !ok {
		t.Fatalf("expected SQLite store, got %T", sqlStore)
	}
	if err := closeSQL(); err != nil {
		t.Fatal(err)
	}
}
