package analyzers

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPythonNoProjectIsNoop(t *testing.T) {
	dir := t.TempDir()
	found, err := NewPython().Analyze(context.Background(), Target{Dir: dir})
	if err != nil || len(found) != 0 {
		t.Fatalf("expected no-op, got %v %v", found, err)
	}
}

func TestTypeScriptNoTsconfigIsNoop(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.ts"), "const x: number = 1\n")
	found, err := NewTypeScript().Analyze(context.Background(), Target{Dir: dir, Files: []string{"a.ts"}})
	if err != nil || len(found) != 0 {
		t.Fatalf("expected no-op without tsconfig, got %v %v", found, err)
	}
}

func TestPythonCompileDetectsSyntaxError(t *testing.T) {
	if _, err := pythonInterpreter(); err != nil {
		t.Skip("no python interpreter")
	}
	if _, err := exec.LookPath("ruff"); err == nil {
		t.Skip("ruff present; compile fallback not exercised")
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "pyproject.toml"), "[project]\nname='x'\n")
	mustWrite(t, filepath.Join(dir, "bad.py"), "def f(:\n    pass\n")
	found, err := NewPython().Analyze(context.Background(), Target{Dir: dir, Files: []string{"bad.py"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("expected a compile finding")
	}
}

func TestFilterExt(t *testing.T) {
	got := filterExt([]string{"a.py", "b.go", "c.py"}, ".py")
	if len(got) != 2 {
		t.Fatalf("unexpected filter result %v", got)
	}
}
