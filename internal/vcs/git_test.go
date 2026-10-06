package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChangesAndFile(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	write(t, filepath.Join(dir, "a.go"), "package a\n")
	write(t, filepath.Join(dir, "b.go"), "package a\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	base := git(t, dir, "rev-parse", "HEAD")

	write(t, filepath.Join(dir, "a.go"), "package a\n\nvar X = 1\n")
	os.Remove(filepath.Join(dir, "b.go"))
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "head")
	head := git(t, dir, "rev-parse", "HEAD")

	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.Changes(superTrim(base), superTrim(head))
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, c := range changes {
		status[c.Path] = c.Status
	}
	if status["a.go"] != "M" || status["b.go"] != "D" {
		t.Fatalf("unexpected changes: %+v", changes)
	}

	content, err := r.File(superTrim(head), "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "package a\n\nvar X = 1\n" {
		t.Fatalf("unexpected content %q", content)
	}
}

func superTrim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
