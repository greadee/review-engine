package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalLayering(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.yaml")
	local := filepath.Join(dir, "revu.yaml")
	writeConfig(t, global, "profile: audit\nstore:\n  sqlite: true\nignore:\n  - \"vendor/**\"\n")
	writeConfig(t, local, "provider:\n  model: local-model\n")
	t.Setenv("REVIEW_GLOBAL_CONFIG", global)

	cfg, err := Load(local)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile != "audit" {
		t.Fatalf("global profile not applied: %s", cfg.Profile)
	}
	if !cfg.Store.SQLite {
		t.Fatal("global store.sqlite not applied")
	}
	if cfg.Provider.Model != "local-model" {
		t.Fatalf("local override not applied: %s", cfg.Provider.Model)
	}
	if len(cfg.Ignore) != 1 || cfg.Ignore[0] != "vendor/**" {
		t.Fatalf("global ignore not applied: %v", cfg.Ignore)
	}
}

func TestApplyProfile(t *testing.T) {
	cfg := Default()
	no := false
	cfg.Profiles = map[string]ProfileOverride{
		"audit": {BlockThreshold: "P2", MaxFiles: 5, Ignore: []string{"a/**"}, Semantic: &no, Rubric: []string{"architecture"}},
	}
	cfg.ApplyProfile("audit")
	if cfg.Review.BlockThreshold != "P2" || cfg.Review.MaxFiles != 5 || cfg.Review.Semantic {
		t.Fatalf("profile override not applied: %+v", cfg.Review)
	}
	if len(cfg.Ignore) != 1 || cfg.Ignore[0] != "a/**" {
		t.Fatalf("ignore override not applied: %v", cfg.Ignore)
	}
	if len(cfg.Review.Rubric) != 1 || cfg.Review.Rubric[0] != "architecture" {
		t.Fatalf("rubric override not applied: %v", cfg.Review.Rubric)
	}
	// Unknown profile is a no-op.
	before := cfg.Review.BlockThreshold
	cfg.ApplyProfile("nope")
	if cfg.Review.BlockThreshold != before {
		t.Fatal("unknown profile should not change config")
	}
}

func TestMarshal(t *testing.T) {
	data, err := Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "provider:") || !strings.Contains(string(data), "sqlite: false") {
		t.Fatalf("unexpected marshaled config:\n%s", data)
	}
}

func TestSQLitePath(t *testing.T) {
	if got := (StoreConfig{}).SQLitePath(".review-state"); got != ".review-state/review.db" {
		t.Fatalf("got %q", got)
	}
	if got := (StoreConfig{Path: "/tmp/x.db"}).SQLitePath(".review-state"); got != "/tmp/x.db" {
		t.Fatalf("got %q", got)
	}
}
