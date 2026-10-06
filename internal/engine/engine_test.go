package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
