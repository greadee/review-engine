package store

import (
	"context"
	"testing"

	"github.com/greadee/review-engine/internal/findings"
)

func TestJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewJSON(dir)
	ctx := context.Background()

	if state, err := s.Load(ctx); err != nil || len(state.Findings) != 0 {
		t.Fatalf("empty load: %+v %v", state, err)
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
}

func TestStateless(t *testing.T) {
	var s Stateless
	if err := s.Save(context.Background(), State{RunID: "r"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(context.Background())
	if err != nil || got.RunID != "" {
		t.Fatalf("stateless should keep nothing: %+v %v", got, err)
	}
}
