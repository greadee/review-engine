package bundle

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/reviewers"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	in := Bundle{
		Version:        Version,
		Profile:        "pr",
		Repository:     "o/r",
		Base:           "a",
		Head:           "b",
		RunID:          "pr@b",
		Generated:      time.Unix(0, 0).UTC(),
		BlockThreshold: "P1",
		Rubric:         []string{"correctness"},
		Files:          []reviewers.FileContent{{Path: "a.go", Content: "package a"}},
		StaticFindings: []findings.Finding{{ID: "x", Title: "t", Severity: findings.P2, Classification: findings.Bug}},
	}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.Profile != "pr" || len(out.Files) != 1 || out.Files[0].Path != "a.go" || len(out.StaticFindings) != 1 {
		t.Fatalf("roundtrip mismatch: %+v", out)
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing bundle")
	}
}
