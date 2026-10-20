package analyzers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoAnalyzerChildEnvExcludesSecrets is the regression test for #10: analyzer
// subprocesses run build tooling over PR-controlled code, so they must not
// inherit the provider key or the GitHub token.
func TestGoAnalyzerChildEnvExcludesSecrets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	dump := filepath.Join(t.TempDir(), "env.txt")
	// Bake the dump path into the script: the scraped child env must not be
	// relied upon to tell the script where to write.
	fake := "#!/bin/sh\nprintenv > '" + dump + "'\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("REVIEW_PROVIDER_API_KEY", "sekret-key-value")
	t.Setenv("GITHUB_TOKEN", "gh-token-value")

	if _, err := NewGo().Analyze(context.Background(), Target{Dir: dir, Files: []string{"x.go"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("fake go was not executed: %v", err)
	}
	env := string(data)
	if !strings.Contains(env, "PATH=") {
		t.Fatalf("child env looks wrong (no PATH):\n%s", env)
	}
	for _, secret := range []string{"REVIEW_PROVIDER_API_KEY", "sekret-key-value", "GITHUB_TOKEN", "gh-token-value"} {
		if strings.Contains(env, secret) {
			t.Errorf("analyzer subprocess inherited %q:\n%s", secret, env)
		}
	}
}
