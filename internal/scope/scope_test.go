package scope

import (
	"testing"

	"github.com/greadee/review-engine/internal/vcs"
)

func TestResolveFilters(t *testing.T) {
	changes := []vcs.Change{
		{Status: "M", Path: "a.go"},
		{Status: "D", Path: "b.go"},
		{Status: "A", Path: "docs/c.md"},
		{Status: "R", OldPath: "old.go", Path: "src/new.go"},
	}
	s := Resolve("pr", changes, []string{"docs/**", "**/*.md"})
	want := map[string]bool{"a.go": true, "src/new.go": true}
	if len(s.Files) != len(want) {
		t.Fatalf("expected %d files, got %v", len(want), s.Files)
	}
	for _, f := range s.Files {
		if !want[f] {
			t.Fatalf("unexpected file %q", f)
		}
	}
}

func TestResolveDedup(t *testing.T) {
	changes := []vcs.Change{{Status: "M", Path: "a.go"}, {Status: "M", Path: "a.go"}}
	if got := Resolve("pr", changes, nil); len(got.Files) != 1 {
		t.Fatalf("expected dedup, got %v", got.Files)
	}
}
