package analyzers

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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

	found, err := NewGo().Analyze(context.Background(), Target{Dir: dir, Files: []string{"main.go"}})
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

func TestGoAnalyzerMissingToolIsUnavailable(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module x\n\ngo 1.26\n")
	t.Setenv("PATH", t.TempDir())
	_, err := NewGo().Analyze(context.Background(), Target{Dir: dir, Files: []string{"x.go"}})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing go tool should be ErrUnavailable, got %v", err)
	}
}

func TestGoPackagesScopesChangedPackages(t *testing.T) {
	got := goPackages([]string{"pkg/a/a.go", "pkg/a/a_test.go", "other/b/b.go", "README.md"}, false)
	want := []string{"./other/b", "./pkg/a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("goPackages = %v, want %v", got, want)
	}
	if whole := goPackages(nil, true); !reflect.DeepEqual(whole, []string{"./..."}) {
		t.Fatalf("whole-repo packages = %v", whole)
	}
}

func TestGofmtHandlesUnusualFilenames(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("no gofmt")
	}
	dir := t.TempDir()
	files := []string{"weird name.go", "-dash.go"}
	mustWrite(t, filepath.Join(dir, files[0]), "package weird\n\nfunc  F( ) {}\n")
	mustWrite(t, filepath.Join(dir, files[1]), "package dash\n\nfunc  D( ) {}\n")
	found, err := runGofmt(context.Background(), dir, files)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range found {
		got[f.Evidence.File] = true
	}
	for _, name := range files {
		if !got[name] {
			t.Fatalf("gofmt did not report %q safely: %+v", name, found)
		}
	}
}

func TestAnalyzerTimeout(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nsleep 1\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := runCheck(ctx, t.TempDir(), check{detector: "static.govet", subcommand: "vet"}, []string{"./..."})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}
