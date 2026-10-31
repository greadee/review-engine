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
	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/issues"
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

func TestCommittedConfigsAreGeneric(t *testing.T) {
	root := filepath.Join("..", "..")
	files := []string{
		filepath.Join(root, "revu.yaml"),
		filepath.Join(root, "revu.example.yaml"),
		filepath.Join(root, "action.yml"),
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"host.docker.internal", "127.0.0.1", "localhost"} {
			if strings.Contains(strings.ToLower(string(data)), forbidden) {
				t.Fatalf("%s contains local endpoint %q", path, forbidden)
			}
		}
	}
}

func TestCommittedAndStarterConfigsLoad(t *testing.T) {
	t.Setenv("REVIEW_GLOBAL_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
	root := filepath.Join("..", "..")
	for _, path := range []string{
		filepath.Join(root, "revu.yaml"),
		filepath.Join(root, "revu.example.yaml"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadData(data); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if _, err := config.LoadData([]byte(starterConfig)); err != nil {
		t.Fatalf("starter config: %v", err)
	}
}

func TestScopedStateDir(t *testing.T) {
	if got := scopedStateDir(".review-state", 0, "pr"); got != ".review-state" {
		t.Fatalf("no-PR scope = %q", got)
	}
	want := filepath.Join(".review-state", "pr-42", "impact")
	if got := scopedStateDir(".review-state", 42, "impact"); got != want {
		t.Fatalf("PR/profile scope = %q, want %q", got, want)
	}
}

func TestLocalScopeUsesMergeBase(t *testing.T) {
	dir := t.TempDir()
	gitTest(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", "a.go")
	gitTest(t, dir, "commit", "-q", "-m", "base")

	gitTest(t, dir, "switch", "-c", "feature")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nvar X = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", "a.go")
	gitTest(t, dir, "commit", "-q", "-m", "feature")
	head := gitTest(t, dir, "rev-parse", "HEAD")

	gitTest(t, dir, "switch", "main")
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package a\n\nvar Y = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", "b.go")
	gitTest(t, dir, "commit", "-q", "-m", "main advance")
	baseTip := gitTest(t, dir, "rev-parse", "HEAD")

	repo, err := vcs.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := localScope(repo, "pr", baseTip, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("localScope = %v, want only a.go", got)
	}
}

func TestScopedSQLitePath(t *testing.T) {
	if got := scopedSQLitePath("/tmp/review.db", 42, "pr"); got != "/tmp/review.pr-42.pr.db" {
		t.Fatalf("db scope = %q", got)
	}
	if got := scopedSQLitePath("/tmp/review", 42, "pr"); got != "/tmp/review.pr-42.pr" {
		t.Fatalf("extensionless db scope = %q", got)
	}
	if got := scopedSQLitePath(":memory:", 42, "pr"); got != ":memory:" {
		t.Fatalf("memory db scope = %q", got)
	}
}

func TestStateScopeRejectsMismatchedPR(t *testing.T) {
	if got, err := stateScope(42, 0); err != nil || got != 42 {
		t.Fatalf("flag-only scope = %d, %v", got, err)
	}
	if got, err := stateScope(0, 42); err != nil || got != 42 {
		t.Fatalf("bundle-only scope = %d, %v", got, err)
	}
	if _, err := stateScope(42, 99); err == nil {
		t.Fatal("mismatched PR must be rejected")
	}
}

func TestIssueEvidenceResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/o/r/issues/12":
			io.WriteString(w, `{"number":12,"state":"closed","pull_request":{"url":"https://api.github.com/repos/o/r/pulls/12"}}`)
		case "/repos/o/r/pulls/12":
			io.WriteString(w, `{"number":12,"state":"closed","merged_at":"2026-10-01T00:00:00Z"}`)
		case "/repos/o/r/pulls/12/files":
			io.WriteString(w, `[{"filename":"internal/auth/login.go","status":"modified"}]`)
		case "/repos/o/r/issues/13":
			io.WriteString(w, "{\"number\":13,\"state\":\"closed\",\"body\":\"Work in `internal/auth`.\"}")
		case "/repos/o/r/issues/14":
			io.WriteString(w, `{"number":14,"state":"open"}`)
		case "/repos/o/r/issues/15":
			io.WriteString(w, `{"number":15,"state":"closed","pull_request":{"url":"https://api.github.com/repos/o/r/pulls/15"}}`)
		case "/repos/o/r/pulls/15":
			io.WriteString(w, `{"number":15,"state":"closed","merged_at":"2026-10-01T00:00:00Z"}`)
		case "/repos/o/r/pulls/15/files":
			io.WriteString(w, `[{"filename":"other/thing.go","status":"modified"}]`)
		case "/repos/o/r/issues/16":
			io.WriteString(w, `{"number":16,"state":"open","pull_request":{"url":"https://api.github.com/repos/o/r/pulls/16"}}`)
		case "/repos/o/r/pulls/16":
			io.WriteString(w, `{"number":16,"state":"open","merged_at":null}`)
		case "/repos/o/r/commits/abcdef1":
			io.WriteString(w, `{"sha":"abcdef1","files":[{"filename":"internal/auth/login.go"}]}`)
		case "/repos/o/r/commits/1234567":
			io.WriteString(w, `{"sha":"1234567","files":[{"filename":"other/thing.go"}]}`)
		case "/repos/o/r/commits/deadbeef":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	gh := vcs.NewGitHub("token")
	gh.BaseURL = srv.URL
	resolver := issueEvidenceResolver(gh, "o", "r")
	areas := []string{"internal/auth"}
	cases := []struct {
		ref    issues.EvidenceRef
		wantOK bool
		want   string
	}{
		{issues.EvidenceRef{Kind: issues.EvRef, Value: "#12"}, true, "merged"},
		{issues.EvidenceRef{Kind: issues.EvRef, Value: "#13"}, true, "closed"},
		{issues.EvidenceRef{Kind: issues.EvRef, Value: "#14"}, false, "not closed"},
		{issues.EvidenceRef{Kind: issues.EvRef, Value: "#15"}, false, "does not touch"},
		{issues.EvidenceRef{Kind: issues.EvRef, Value: "#16"}, false, "not merged"},
		{issues.EvidenceRef{Kind: issues.EvRef, Value: "abcdef1"}, true, "exists"},
		{issues.EvidenceRef{Kind: issues.EvRef, Value: "1234567"}, false, "does not touch"},
		{issues.EvidenceRef{Kind: issues.EvRef, Value: "deadbeef"}, false, "does not resolve"},
	}
	for _, tc := range cases {
		ok, reason := resolver(context.Background(), tc.ref, areas)
		if ok != tc.wantOK || !strings.Contains(reason, tc.want) {
			t.Errorf("%s: got (%v, %q), want (%v, reason containing %q)", tc.ref.Value, ok, reason, tc.wantOK, tc.want)
		}
	}
}

func TestReconcileCommentSanitizesUntrustedText(t *testing.T) {
	v := issues.Verdict{
		Criteria: 1,
		Details: []issues.Criterion{{
			Text: "<img src=x> @team `breakout`",
			Done: true,
			Checks: []issues.EvidenceCheck{{
				Ref:    issues.EvidenceRef{Kind: issues.EvRef, Value: "#1`<script>"},
				Reason: "<b>@here</b>",
			}},
		}},
	}
	body := reconcileComment(vcs.Issue{Number: 1}, v)
	for _, raw := range []string{"<img", "@team", "<script>", "<b>", "@here", "`breakout`", "#1`"} {
		if strings.Contains(body, raw) {
			t.Fatalf("raw %q leaked into reconcile comment:\n%s", raw, body)
		}
	}
}

func TestRelatedFindingsCommentSanitizesUntrustedText(t *testing.T) {
	body := relatedFindingsComment(vcs.Issue{Number: 1}, []findings.Finding{{
		Title:    "<script>@team `breakout`",
		Severity: findings.P2,
		Evidence: findings.Evidence{File: "a`b.go", Line: 1},
	}})
	for _, raw := range []string{"<script>", "@team", "`breakout`", "a`b.go"} {
		if strings.Contains(body, raw) {
			t.Fatalf("raw %q leaked into related-findings comment:\n%s", raw, body)
		}
	}
}
