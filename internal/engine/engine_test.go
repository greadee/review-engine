package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greadee/review-engine/internal/bundle"
	"github.com/greadee/review-engine/internal/config"
	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/provider"
	"github.com/greadee/review-engine/internal/reviewers"
	"github.com/greadee/review-engine/internal/store"
	"github.com/greadee/review-engine/internal/vcs"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func setupRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	git(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	base = strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nvar X = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "head")
	head = strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	return dir, base, head
}

func newEngine(t *testing.T, dir string, fake *provider.Fake, st store.Store) *Engine {
	t.Helper()
	cfg := config.Default()
	cfg.Review.Static = false
	cfg.Review.Semantic = true
	cfg.Review.MaxFiles = 10
	repo, err := vcs.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{
		Config:   cfg,
		Repo:     repo,
		Provider: fake,
		Reviewer: reviewers.New(fake, "model"),
		Store:    st,
	})
}

func TestRunRangeTracksLifecycle(t *testing.T) {
	dir, base, head := setupRepo(t)
	fake := &provider.Fake{Responses: []string{`{"findings":[{"title":"bug","classification":"Bug","severity":"P1","file":"a.go","line":3,"anchor":"bug-x"}]}`}}
	eng := newEngine(t, dir, fake, store.NewJSON(t.TempDir()))
	ctx := context.Background()

	res, err := eng.RunRange(ctx, "pr", base, head, "o/r", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(res.Findings))
	}
	if res.Findings[0].Status != findings.StatusNew {
		t.Fatalf("first run should be new, got %s", res.Findings[0].Status)
	}
	if len(res.Run.Blocking()) != 1 {
		t.Fatalf("expected blocking finding")
	}

	res2, err := eng.RunRange(ctx, "pr", base, head, "o/r", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Findings[0].Status != findings.StatusOngoing {
		t.Fatalf("second run should be ongoing, got %s", res2.Findings[0].Status)
	}
}

func TestRunRangeResolvesVanishedFinding(t *testing.T) {
	dir, base, head := setupRepo(t)
	fake := &provider.Fake{Responses: []string{
		`{"findings":[{"title":"bug","classification":"Bug","severity":"P1","file":"a.go","line":3,"anchor":"bug-x"}]}`,
		`{"findings":[]}`,
	}}
	eng := newEngine(t, dir, fake, store.NewJSON(t.TempDir()))
	ctx := context.Background()

	if _, err := eng.RunRange(ctx, "pr", base, head, "o/r", "HEAD"); err != nil {
		t.Fatal(err)
	}
	res, err := eng.RunRange(ctx, "pr", base, head, "o/r", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 0 || len(res.Run.Resolved) != 1 {
		t.Fatalf("expected 1 resolved, got findings=%d resolved=%d", len(res.Findings), len(res.Run.Resolved))
	}
	if res.Run.Delta().Resolved != 1 {
		t.Fatalf("delta should count resolved: %+v", res.Run.Delta())
	}
}

func TestDetectorsWired(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	writeFile(t, dir, "pkg/x.go", "package pkg\n\nfunc Orphan() {}\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	base := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	writeFile(t, dir, "pkg/x_test.go", "package pkg\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { Orphan() }\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "head")
	head := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	cfg := config.Default()
	cfg.Review.Static = true
	cfg.Review.Semantic = false
	repo, _ := vcs.Open(dir)
	// Analyzers left nil so engine.New installs static analyzers + detectors.
	eng := New(Options{Config: cfg, Repo: repo, Store: store.Stateless{}})

	res, err := eng.RunRange(context.Background(), "audit", base, head, "o/r", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range res.Findings {
		if f.Detector == "detector.orphan" && f.Anchor == "orphan:Orphan" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected orphan detector finding, got %+v", res.Findings)
	}
}

func TestCollectFinalizeSplit(t *testing.T) {
	dir, base, head := setupRepo(t)
	fake := &provider.Fake{Responses: []string{`{"findings":[{"title":"bug","classification":"Bug","severity":"P1","file":"a.go","line":3,"anchor":"bug-x"}]}`}}
	eng := newEngine(t, dir, fake, store.NewJSON(t.TempDir()))
	ctx := context.Background()

	b, err := eng.Collect(ctx, "pr", base, head, "o/r", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if fake.Calls() != 0 {
		t.Fatal("collect must not call the provider")
	}
	if len(b.Files) != 1 || b.Files[0].Path != "a.go" {
		t.Fatalf("collect should carry file contents: %+v", b.Files)
	}
	if b.RunID == "" || b.Profile != "pr" {
		t.Fatalf("bundle metadata missing: %+v", b)
	}

	res, err := eng.Finalize(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 || res.Findings[0].Anchor != "bug-x" {
		t.Fatalf("finalize should apply semantic review: %+v", res.Findings)
	}
}

func TestFinalizeFailsClosedWithoutProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Review.Semantic = true
	eng := New(Options{Config: cfg, Store: store.Stateless{}})
	b := bundle.Bundle{Version: bundle.Version, Profile: "pr", RunID: "pr@x"}
	if _, err := eng.Finalize(context.Background(), b); err == nil {
		t.Fatal("expected fail-closed error when semantic is enabled without a provider")
	}
}

func TestRunIssueGatesClosedIssue(t *testing.T) {
	cfg := config.Default()
	cfg.Review.Static = false
	cfg.Review.Semantic = false
	eng := New(Options{Config: cfg, Store: store.Stateless{}})

	closed := vcs.Issue{Number: 9, State: "closed", Body: "## Acceptance criteria\n\n- [ ] shipped\n- [x] documented\n"}
	res, err := eng.RunIssue(context.Background(), closed)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Run.Blocking()) != 1 {
		t.Fatalf("expected 1 blocking criterion, got %+v", res.Findings)
	}

	open := vcs.Issue{Number: 9, State: "open", Body: closed.Body}
	res, err = eng.RunIssue(context.Background(), open)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Run.Blocking()) != 0 {
		t.Fatalf("open issue should not block, got %+v", res.Run.Blocking())
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunRangeStateless(t *testing.T) {
	dir, base, head := setupRepo(t)
	fake := &provider.Fake{Responses: []string{`{"findings":[]}`}}
	cfg := config.Default()
	cfg.Review.Static = false
	cfg.Mode = config.ModeStateless
	repo, _ := vcs.Open(dir)
	eng := New(Options{Config: cfg, Repo: repo, Provider: fake, Reviewer: reviewers.New(fake, "m"), Store: store.Stateless{}})

	res, err := eng.RunRange(context.Background(), "pr", base, head, "o/r", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(res.Findings))
	}
	if res.Run.Recommendation() != "Approve" {
		t.Fatalf("expected Approve, got %s", res.Run.Recommendation())
	}
}
