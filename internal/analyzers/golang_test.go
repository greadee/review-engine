package analyzers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeNoModule(t *testing.T) {
	found, err := NewGo().Analyze(context.Background(), Target{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("expected no-op without go.mod, got %d", len(found))
	}
}

func TestAnalyzeGofmt(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.com/x\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n\nfunc  main( ) {}\n")

	found, err := NewGo().Analyze(context.Background(), Target{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	var gofmtFound bool
	for _, f := range found {
		if f.Detector == "static.gofmt" {
			gofmtFound = true
			if f.Severity == "" || f.Classification == "" {
				t.Fatalf("finding missing severity/classification: %+v", f)
			}
		}
	}
	if !gofmtFound {
		t.Fatalf("expected a gofmt finding, got %+v", found)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
