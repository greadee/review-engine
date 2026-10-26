package main

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greadee/review-engine/internal/bundle"
	"github.com/greadee/review-engine/internal/config"
	"github.com/greadee/review-engine/internal/provider"
	"github.com/greadee/review-engine/internal/vcs"
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

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@e"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestLoadTrustedConfigUsesBaseRevision(t *testing.T) {
	t.Setenv("REVIEW_GLOBAL_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
	dir := t.TempDir()
	gitTest(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "revu.yaml"), []byte("ignore:\n  - docs/**\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", "revu.yaml")
	gitTest(t, dir, "commit", "-q", "-m", "base")
	base := gitTest(t, dir, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(dir, "revu.yaml"), []byte("ignore:\n  - '**/*'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", "revu.yaml")
	gitTest(t, dir, "commit", "-q", "-m", "head")

	repo, err := vcs.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loadTrustedConfig(repo, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Ignore) != 1 || cfg.Ignore[0] != "docs/**" {
		t.Fatalf("trusted config should come from base, got %v", cfg.Ignore)
	}
	_headCfg, err := loadTrustedConfig(repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(_headCfg.Ignore) != 1 || _headCfg.Ignore[0] != "**/*" {
		t.Fatalf("HEAD config should be the later file, got %v", _headCfg.Ignore)
	}
}

func TestGitHubScopeAppliesTrustedIgnore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/7/files" {
			_, _ = w.Write([]byte(`[{"filename":"a.go","status":"modified"},{"filename":"docs/x.md","status":"added"}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	gh := vcs.NewGitHub("token")
	gh.BaseURL = srv.URL
	b := bundle.Bundle{Profile: "pr", PR: 7}
	got, err := githubScope(context.Background(), gh, "o", "r", b, []string{"docs/**"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("expected only a.go, got %v", got)
	}
	if _, err := githubScope(context.Background(), gh, "o", "r", b, []string{"**/*"}); err == nil {
		t.Fatal("expected an empty trusted scope to be rejected")
	}
}

func TestTrustedBaseConfigUsesPRBaseRevision(t *testing.T) {
	content := base64.StdEncoding.EncodeToString([]byte("ignore:\n  - docs/**\n"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/7":
			_, _ = w.Write([]byte(`{"head":{"sha":"headsha"},"merge_commit_sha":"mergesha","base":{"sha":"basesha"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/contents/revu.yaml":
			if r.URL.Query().Get("ref") != "basesha" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"content":"` + content + `","encoding":"base64"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	gh := vcs.NewGitHub("token")
	gh.BaseURL = srv.URL
	b := bundle.Bundle{Profile: "pr", PR: 7, Head: "headsha"}
	cfg, err := trustedBaseConfig(context.Background(), gh, "o", "r", b)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Ignore) != 1 || cfg.Ignore[0] != "docs/**" {
		t.Fatalf("unexpected trusted config: %v", cfg.Ignore)
	}

	b.Head = "other"
	if _, err := trustedBaseConfig(context.Background(), gh, "o", "r", b); err == nil {
		t.Fatal("expected head mismatch to be rejected")
	}
}

func TestCurrentVersionUsesInjectedVersion(t *testing.T) {
	old := version
	version = "1.2.0"
	defer func() { version = old }()
	if got := currentVersion(); got != "1.2.0" {
		t.Fatalf("currentVersion() = %q, want 1.2.0", got)
	}
}

func TestRunAcceptsStaticAndSemanticFlags(t *testing.T) {
	t.Setenv("REVIEW_GLOBAL_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
	dir := t.TempDir()
	gitTest(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", "a.go")
	gitTest(t, dir, "commit", "-q", "-m", "base")
	base := gitTest(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nvar X = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", "a.go")
	gitTest(t, dir, "commit", "-q", "-m", "head")
	head := gitTest(t, dir, "rev-parse", "HEAD")

	args := []string{
		"--repo-dir", dir, "--base", base, "--head", head, "--profile", "pr",
		"--dry-run", "--no-block", "--quiet", "--persist", "never",
		"--static=false", "--semantic=false",
	}
	if err := run(args); err != nil {
		t.Fatalf("run with documented flags failed: %v", err)
	}
	if err := run(append(args[:len(args)-2], "--static=not-bool")); err == nil {
		t.Fatal("expected invalid --static value to fail")
	}
}

func TestShowConfigRedactsGitHubToken(t *testing.T) {
	t.Setenv("REVIEW_GLOBAL_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
	path := filepath.Join(t.TempDir(), "revu.yaml")
	if err := os.WriteFile(path, []byte("githubToken: supersecret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	callErr := showConfig([]string{"--config", path})
	_ = w.Close()
	os.Stdout = old
	data, _ := io.ReadAll(r)
	if callErr != nil {
		t.Fatal(callErr)
	}
	if strings.Contains(string(data), "supersecret") {
		t.Fatalf("revu config leaked the token:\n%s", data)
	}
}
