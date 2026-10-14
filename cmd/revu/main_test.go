package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/greadee/review-engine/internal/config"
	"github.com/greadee/review-engine/internal/provider"
)

// TestPRConfigCannotRedirectProviderKey is the regression test for the
// same-repository PR path: a revu.yaml that points provider.baseUrl at an
// attacker's endpoint must not receive the API key.
func TestPRConfigCannotRedirectProviderKey(t *testing.T) {
	t.Setenv(provider.AllowedBaseURLsEnv, "")
	t.Setenv("REVIEW_PROVIDER_API_KEY", "sekret")
	t.Setenv("REVIEW_GLOBAL_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))

	cfg := config.Default()
	cfg.Provider.BaseURL = "https://attacker.example/v1"

	if _, err := buildProvider(cfg, false); err == nil {
		t.Fatal("expected refusal to attach the provider key to an unlisted endpoint")
	}

	// A workflow-controlled allowlist (env, not revu.yaml) may permit it.
	t.Setenv(provider.AllowedBaseURLsEnv, "https://attacker.example/v1")
	if _, err := buildProvider(cfg, false); err != nil {
		t.Fatalf("allowlisted endpoint should be permitted: %v", err)
	}
}

// TestRepoConfigAllowlistIsIgnored proves the endpoint allowlist cannot be
// widened from the (pull-request-controlled) repository config.
func TestRepoConfigAllowlistIsIgnored(t *testing.T) {
	t.Setenv(provider.AllowedBaseURLsEnv, "")
	t.Setenv("REVIEW_PROVIDER_API_KEY", "sekret")
	t.Setenv("REVIEW_GLOBAL_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))

	cfg := config.Default()
	cfg.Provider.BaseURL = "https://attacker.example/v1"
	cfg.Provider.AllowedBaseURLs = []string{"https://attacker.example/v1"}

	if _, err := buildProvider(cfg, false); err == nil {
		t.Fatal("repository config must not be able to allowlist its own endpoint")
	}
}

// TestDefaultEndpointIsTrusted verifies the built-in endpoint needs no
// allowlisting, so the common case is unaffected.
func TestDefaultEndpointIsTrusted(t *testing.T) {
	t.Setenv(provider.AllowedBaseURLsEnv, "")
	t.Setenv("REVIEW_PROVIDER_API_KEY", "sekret")

	cfg := config.Default()
	cfg.Provider.BaseURL = provider.DefaultBaseURL
	if _, err := buildProvider(cfg, false); err != nil {
		t.Fatalf("default endpoint should be allowed: %v", err)
	}

	cfg.Provider.BaseURL = ""
	if _, err := buildProvider(cfg, false); err != nil {
		t.Fatalf("empty base URL means the default and should be allowed: %v", err)
	}
}

// TestGlobalConfigCanAllowlist verifies the (trusted) global config can
// allowlist an endpoint.
func TestGlobalConfigCanAllowlist(t *testing.T) {
	t.Setenv(provider.AllowedBaseURLsEnv, "")
	t.Setenv("REVIEW_PROVIDER_API_KEY", "sekret")

	global := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(global, []byte("provider:\n  allowedBaseUrls:\n    - https://internal.example/v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REVIEW_GLOBAL_CONFIG", global)

	cfg := config.Default()
	cfg.Provider.BaseURL = "https://internal.example/v1"
	if _, err := buildProvider(cfg, false); err != nil {
		t.Fatalf("global-config allowlist should permit the endpoint: %v", err)
	}
}
