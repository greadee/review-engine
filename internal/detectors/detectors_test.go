package detectors

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/greadee/review-engine/internal/analyzers"
	"github.com/greadee/review-engine/internal/findings"
)

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func analyze(t *testing.T, a analyzers.Analyzer, target analyzers.Target) []findings.Finding {
	t.Helper()
	got, err := a.Analyze(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func target(dir string, files []string) analyzers.Target {
	return analyzers.Target{Dir: dir, Files: files, AllFiles: files, WholeRepo: true}
}

func TestOrphanGo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "pkg/wired.go", "package pkg\n\nfunc Wired() {}\n")
	write(t, dir, "pkg/testonly.go", "package pkg\n\nfunc TestOnly() {}\n")
	write(t, dir, "pkg/unused.go", "package pkg\n\nfunc Unused() {}\n")
	write(t, dir, "pkg/use.go", "package pkg\n\nfunc Use() { Wired() }\n")
	write(t, dir, "pkg/wired_test.go", "package pkg\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { TestOnly() }\n")

	files := []string{"pkg/wired.go", "pkg/testonly.go", "pkg/unused.go", "pkg/use.go", "pkg/wired_test.go"}
	got := analyze(t, NewOrphanGo(), target(dir, files))

	byAnchor := map[string]findings.Finding{}
	for _, f := range got {
		byAnchor[f.Anchor] = f
	}
	if _, ok := byAnchor["orphan:Wired"]; ok {
		t.Fatal("Wired is used in production and should not be flagged")
	}
	if f, ok := byAnchor["orphan:TestOnly"]; !ok || f.Severity != findings.P2 {
		t.Fatalf("TestOnly should be flagged P2 test-only: %+v", byAnchor)
	}
	if f, ok := byAnchor["orphan:Unused"]; !ok || f.Severity != findings.P3 {
		t.Fatalf("Unused should be flagged P3: %+v", byAnchor)
	}
}

func TestStaleReference(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "docs/a.md", "See `docs/present.md` and `docs/missing.md`.\n")
	write(t, dir, "docs/present.md", "# present\n")
	got := analyze(t, NewStaleReference(), analyzers.Target{Dir: dir, AllFiles: []string{"docs/a.md", "docs/present.md"}})
	if len(got) != 1 {
		t.Fatalf("expected 1 stale ref, got %d: %+v", len(got), got)
	}
	if got[0].Anchor != "stale:docs/missing.md" {
		t.Fatalf("unexpected anchor %q", got[0].Anchor)
	}
}

func TestFailOpen(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "s.go", "package s\n\nfunc (c checker) run() error {\n\tif c.policy != nil && !c.policy.Allowed() {\n\t\treturn err\n\t}\n\treturn nil\n}\n")
	got := analyze(t, NewFailOpen(), analyzers.Target{Dir: dir, Files: []string{"s.go"}, AllFiles: []string{"s.go"}})
	found := false
	for _, f := range got {
		if f.Detector == "detector.fail-open" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected fail-open finding, got %+v", got)
	}
}

func TestUnfinished(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "u.go", "package u\n\nfunc F() {\n\tpanic(\"not implemented\")\n}\n")
	write(t, dir, "t.go", "package u\n\n// TODO: later\n")
	got := analyze(t, NewUnfinished(), analyzers.Target{Dir: dir, Files: []string{"u.go", "t.go"}, AllFiles: []string{"u.go", "t.go"}})
	sev := map[string]findings.Severity{}
	for _, f := range got {
		sev[f.Evidence.File] = f.Severity
	}
	if sev["u.go"] != findings.P2 {
		t.Fatalf("not-implemented should be P2: %+v", got)
	}
	if sev["t.go"] != findings.P3 {
		t.Fatalf("TODO should be P3: %+v", got)
	}
}
