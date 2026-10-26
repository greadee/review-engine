package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndFile(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile != "pr" || cfg.Mode != ModeTracking || cfg.Provider.Name != "openai-compatible" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "revu.yaml")
	if err := os.WriteFile(path, []byte("profile: audit\nreview:\n  static: false\nignore:\n  - vendor/**\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile != "audit" || cfg.Review.Static {
		t.Fatalf("file not applied: %+v", cfg)
	}
	if len(cfg.Ignore) != 1 || cfg.Ignore[0] != "vendor/**" {
		t.Fatalf("ignore not applied: %v", cfg.Ignore)
	}
}

func TestLoadDataUsesEnvOverrides(t *testing.T) {
	t.Setenv("REVIEW_SEMANTIC", "false")
	cfg, err := LoadData([]byte("profile: audit\nignore:\n  - docs/**\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile != "audit" || cfg.Review.Semantic {
		t.Fatalf("data/env layering failed: %+v", cfg)
	}
	if len(cfg.Ignore) != 1 || cfg.Ignore[0] != "docs/**" {
		t.Fatalf("ignore not applied: %v", cfg.Ignore)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("REVIEW_PROFILE", "sprint")
	t.Setenv("REVIEW_MODEL", "env-model")
	t.Setenv("REVIEW_SEMANTIC", "false")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile != "sprint" || cfg.Provider.Model != "env-model" || cfg.Review.Semantic {
		t.Fatalf("env overrides not applied: %+v", cfg)
	}
}

func TestAPIKeyFromEnv(t *testing.T) {
	t.Setenv("MY_KEY", "abc")
	p := ProviderConfig{APIKeyEnv: "MY_KEY"}
	if p.APIKey() != "abc" {
		t.Fatalf("got %q", p.APIKey())
	}
}

func TestRedactedClearsCredentials(t *testing.T) {
	cfg := Default()
	cfg.GitHubToken = "secret-token"
	redacted := cfg.Redacted()
	if redacted.GitHubToken != "" {
		t.Fatalf("github token was not redacted: %q", redacted.GitHubToken)
	}
	if cfg.GitHubToken != "secret-token" {
		t.Fatal("Redacted mutated the original config")
	}
}
