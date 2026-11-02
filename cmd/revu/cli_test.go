package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greadee/review-engine/internal/bundle"
	"github.com/greadee/review-engine/internal/reviewers"
)

func cliRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	t.Setenv("REVIEW_GLOBAL_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
	// Keep CLI state paths deterministic when the test suite runs inside CI.
	t.Setenv("GITHUB_ACTIONS", "")
	dir = t.TempDir()
	gitTest(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", ".")
	gitTest(t, dir, "commit", "-q", "-m", "base")
	base = gitTest(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nvar X = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", ".")
	gitTest(t, dir, "commit", "-q", "-m", "head")
	head = gitTest(t, dir, "rev-parse", "HEAD")
	return dir, base, head
}

func quietDispatch(t *testing.T, args []string) int {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = devnull, devnull
	code := dispatch(args)
	os.Stdout, os.Stderr = oldOut, oldErr
	_ = devnull.Close()
	return code
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCLICommands(t *testing.T) {
	dir, base, head := cliRepo(t)
	tmp := t.TempDir()
	bundleOut := filepath.Join(tmp, "collected.json")
	finalizeOut := filepath.Join(tmp, "final.md")
	findingsPath := filepath.Join(tmp, "findings.json")
	writeCLIFile(t, findingsPath, `{"id":"audit@abc","profile":"audit","findings":[{"title":"sample","classification":"Bug","severity":"P2","evidence":{"file":"a.go","line":1},"detector":"detector.sample","anchor":"sample"}]}`)
	initPath := filepath.Join(tmp, "starter.yaml")
	configPath := filepath.Join(tmp, "revu.yaml")
	writeCLIFile(t, configPath, "profile: audit\n")
	reportOut := filepath.Join(tmp, "report.md")
	bundlePath := filepath.Join(tmp, "input.json")
	b := bundle.Bundle{
		Version:        bundle.Version,
		Profile:        "audit",
		RunID:          "audit@abc",
		BlockThreshold: "P1",
		Selected:       1,
		Files:          []reviewers.FileContent{{Path: "a.go", Content: "package a\n"}},
	}
	if err := bundle.Save(bundlePath, b); err != nil {
		t.Fatal(err)
	}
	stateAlways := filepath.Join(tmp, "state-always")
	stateNever := filepath.Join(tmp, "state-never")
	corpus := filepath.Join("..", "..", "testdata", "corpus")

	cases := []struct {
		name string
		args []string
		want int
	}{
		{name: "no command", args: nil, want: 2},
		{name: "unknown command", args: []string{"not-a-command"}, want: 2},
		{name: "version", args: []string{"version"}, want: 0},
		{name: "providers", args: []string{"providers"}, want: 0},
		{name: "help", args: []string{"help"}, want: 0},
		{name: "plan", args: []string{"plan", "--repo-dir", dir, "--base", base, "--head", head, "--profile", "pr"}, want: 0},
		{name: "collect", args: []string{"collect", "--repo-dir", dir, "--base", base, "--head", head, "--profile", "pr", "--out", bundleOut}, want: 0},
		{name: "run dry-run", args: []string{"run", "--repo-dir", dir, "--base", base, "--head", head, "--profile", "pr", "--dry-run", "--no-block", "--quiet", "--persist", "never", "--static=false", "--semantic=false"}, want: 0},
		{name: "run persist always", args: []string{"run", "--repo-dir", dir, "--base", base, "--head", head, "--profile", "pr", "--no-block", "--quiet", "--persist", "always", "--static=false", "--semantic=false", "--state-dir", stateAlways}, want: 0},
		{name: "run persist never", args: []string{"run", "--repo-dir", dir, "--base", base, "--head", head, "--profile", "pr", "--no-block", "--quiet", "--persist", "never", "--static=false", "--semantic=false", "--state-dir", stateNever}, want: 0},
		{name: "finalize", args: []string{"finalize", "--repo-dir", dir, "--bundle", bundlePath, "--dry-run", "--no-block", "--out", finalizeOut}, want: 0},
		{name: "report", args: []string{"report", "--in", findingsPath, "--out", reportOut, "--audit"}, want: 0},
		{name: "init", args: []string{"init", "--path", initPath}, want: 0},
		{name: "config", args: []string{"config", "--config", configPath}, want: 0},
		{name: "file-issue dry-run", args: []string{"file-issue", "--in", findingsPath, "--dry-run"}, want: 0},
		{name: "eval check", args: []string{"eval", "--corpus", corpus, "--check"}, want: 0},
		{name: "config error", args: []string{"collect", "--repo-dir", dir, "--base", base, "--profile", "not-a-profile"}, want: 1},
		{name: "invalid bool flag", args: []string{"run", "--repo-dir", dir, "--base", base, "--head", head, "--dry-run", "--static=not-bool"}, want: 1},
		{name: "missing bundle", args: []string{"finalize", "--bundle", filepath.Join(tmp, "absent.json"), "--dry-run"}, want: 1},
		{name: "missing issue", args: []string{"issue", "--issue", "0"}, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := quietDispatch(t, tc.args); got != tc.want {
				t.Fatalf("exit code = %d, want %d", got, tc.want)
			}
		})
	}

	for _, path := range []string{bundleOut, finalizeOut, findingsPath, initPath, reportOut, configPath} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s to exist: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(stateAlways, "state.json")); err != nil {
		t.Errorf("persist always should write state: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateNever, "state.json")); !os.IsNotExist(err) {
		t.Errorf("persist never wrote state: %v", err)
	}
}

func TestIssueCommandCommentAndClosePaths(t *testing.T) {
	dir, _, _ := cliRepo(t)
	var posted, closed bool
	var commentBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/1":
			_, _ = io.WriteString(w, "{\"number\":1,\"state\":\"open\",\"body\":\"## Acceptance criteria\\n\\n- [x] done — `a.go`\\n\"}")
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/1/comments":
			_, _ = io.WriteString(w, `[]`)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/1/comments":
			data, _ := io.ReadAll(r.Body)
			commentBody = string(data)
			posted = true
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":1}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/o/r/issues/1":
			closed = true
			_, _ = io.WriteString(w, `{"number":1,"state":"closed"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "token")

	args := []string{"issue", "--repository", "o/r", "--issue", "1", "--repo-dir", dir, "--state-dir", t.TempDir(), "--comment", "--close"}
	if got := quietDispatch(t, args); got != 0 {
		t.Fatalf("issue command exit code = %d, want 0", got)
	}
	if !posted || !closed {
		t.Fatalf("expected comment and close calls, posted=%v closed=%v", posted, closed)
	}
	if !strings.Contains(commentBody, "Review Summary") {
		t.Fatalf("comment body missing summary: %s", commentBody)
	}
}

func TestReconcileCommandCommentPath(t *testing.T) {
	dir, _, _ := cliRepo(t)
	var posted bool
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/1":
			_, _ = io.WriteString(w, `{"number":1,"state":"open","body":"## Acceptance criteria\n\n- [x] incomplete\n"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/1/comments":
			_, _ = io.WriteString(w, `[]`)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/1/comments":
			posted = true
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":1}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "")
	configPath := filepath.Join(t.TempDir(), "revu.yaml")
	writeCLIFile(t, configPath, "githubToken: configured-token\n")

	args := []string{"reconcile", "--repository", "o/r", "--issues", "1", "--repo-dir", dir, "--comment", "--config", configPath}
	if got := quietDispatch(t, args); got != 0 {
		t.Fatalf("reconcile command exit code = %d, want 0", got)
	}
	if !posted {
		t.Fatal("expected reconcile comment")
	}
	if gotAuth != "Bearer configured-token" {
		t.Fatalf("reconcile Authorization = %q, want configured token", gotAuth)
	}
}

func TestFileIssueUsesConfiguredToken(t *testing.T) {
	dir, _, _ := cliRepo(t)
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues" {
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"number":7}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "")

	findingsPath := filepath.Join(t.TempDir(), "findings.json")
	writeCLIFile(t, findingsPath, `{"id":"audit@abc","profile":"audit","findings":[{"title":"sample","classification":"Bug","severity":"P2","evidence":{"file":"a.go","line":1},"detector":"detector.sample","anchor":"sample"}]}`)
	configPath := filepath.Join(t.TempDir(), "revu.yaml")
	writeCLIFile(t, configPath, "githubToken: configured-token\n")

	args := []string{"file-issue", "--in", findingsPath, "--repository", "o/r", "--repo-dir", dir, "--config", configPath}
	if got := quietDispatch(t, args); got != 0 {
		t.Fatalf("file-issue exit code = %d, want 0", got)
	}
	if gotAuth != "Bearer configured-token" {
		t.Fatalf("file-issue Authorization = %q, want configured token", gotAuth)
	}
}

func TestEvalUsesEndpointAllowlist(t *testing.T) {
	t.Setenv("REVIEW_GLOBAL_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
	t.Setenv("REVIEW_PROVIDER_API_KEY", "key")
	t.Setenv("REVIEW_PROVIDER_BASE_URL", "https://not-allowed.example/v1")
	t.Setenv("REVIEW_PROVIDER_ALLOWED_BASE_URLS", "")
	corpus := filepath.Join("..", "..", "testdata", "corpus")
	args := []string{"eval", "--corpus", corpus, "--provider", "openai-compatible", "--model", "m"}
	if got := quietDispatch(t, args); got != 1 {
		t.Fatalf("eval with unlisted endpoint exit code = %d, want 1", got)
	}
}
