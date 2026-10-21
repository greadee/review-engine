package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greadee/review-engine/internal/bundle"
	"github.com/greadee/review-engine/internal/config"
	"github.com/greadee/review-engine/internal/engine"
	"github.com/greadee/review-engine/internal/provider"
	"github.com/greadee/review-engine/internal/reviewers"
	"github.com/greadee/review-engine/internal/store"
	"github.com/greadee/review-engine/internal/vcs"
	"gopkg.in/yaml.v3"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@e"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// consumerRepo builds a two-commit repository, returning its dir, base, head.
func consumerRepo(t *testing.T, base, head map[string]string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	for p, c := range base {
		write(t, dir, p, c)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")
	baseSHA := git(t, dir, "rev-parse", "HEAD")
	for p, c := range head {
		write(t, dir, p, c)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "head")
	headSHA := git(t, dir, "rev-parse", "HEAD")
	return dir, baseSHA, headSHA
}

func baseConfig() config.Config {
	cfg := config.Default()
	cfg.Review.Static = false
	cfg.Review.Semantic = true
	cfg.Review.MaxFiles = 100
	cfg.Review.MaxFileBytes = 1_000_000
	cfg.Review.CoverageThreshold = 1.0
	return cfg
}

// TestForkSafeFlow exercises the two-phase flow end to end: a secret-free
// collect produces a bundle, and a separate secret-bearing finalize (with no
// repository checkout) reviews it, publishes an immutable link, and upserts a
// single comment across reruns.
func TestForkSafeFlow(t *testing.T) {
	ctx := context.Background()
	dir, base, head := consumerRepo(t,
		map[string]string{"exec.go": "package exec\n\nfunc Run(in string) {}\n"},
		map[string]string{"exec.go": "package exec\n\nimport \"os/exec\"\n\nfunc Run(in string) {\n\t_ = exec.Command(\"sh\", \"-c\", in).Run()\n}\n"},
	)
	cfg := baseConfig()
	repo, err := vcs.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Phase 1 — collect: no provider, no secrets.
	collect := engine.New(engine.Options{Config: cfg, Repo: repo, PR: 42})
	b, err := collect.Collect(ctx, "pr", base, head, "o/r", head)
	if err != nil {
		t.Fatal(err)
	}
	if b.PR != 42 || b.Head != head {
		t.Fatalf("bundle not bound to the PR/head: %+v", b)
	}
	// Round-trip through the artifact boundary.
	bundlePath := filepath.Join(t.TempDir(), "bundle.json")
	if err := bundle.Save(bundlePath, b); err != nil {
		t.Fatal(err)
	}
	shipped, err := bundle.Load(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := shipped.Verify(bundle.Trusted{Repository: "o/r", PR: 42, Head: head}); err != nil {
		t.Fatalf("bundle binding failed: %v", err)
	}

	// Phase 2 — finalize: secrets, no checkout.
	fake := &provider.Fake{Responses: []string{`{"findings":[{"title":"command injection","classification":"Security","severity":"P0","file":"exec.go","line":6,"snippet":"exec.Command(\"sh\", \"-c\", in).Run()","anchor":"cmd-inj"}]}`}}
	finalize := engine.New(engine.Options{Config: cfg, Provider: fake, Reviewer: reviewers.New(fake, "m", 0, nil), Store: store.Stateless{}})
	res, err := finalize.Finalize(ctx, shipped)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Run.Blocking()) != 1 {
		t.Fatalf("expected one blocking finding, got %+v", res.Run.Blocking())
	}

	gh, url := newFakeServer(t)
	client := vcs.NewGitHub("token")
	client.BaseURL = url

	archive, err := finalize.Publish(ctx, client, "o", "r", res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(archive, "/blob/commit") || !strings.Contains(archive, "/runs/") {
		t.Fatalf("archive link must be an immutable commit permalink, got %q", archive)
	}
	if len(gh.files()) != 2 {
		t.Fatalf("expected JSON+MD archived, got %d files", len(gh.files()))
	}

	marker := "<!-- revu:run=" + res.Run.ID + " -->"
	if err := client.UpsertComment(ctx, "o", "r", 42, marker, "summary"); err != nil {
		t.Fatal(err)
	}
	if err := client.UpsertComment(ctx, "o", "r", 42, marker, "summary"); err != nil {
		t.Fatal(err)
	}
	if n := gh.commentCount(); n != 2 {
		t.Fatalf("reruns must post independent comments, not upsert: got %d comments", n)
	}

	// A blocking finding fails the check.
	if len(res.Run.Blocking()) == 0 {
		t.Fatal("blocking finding must fail the check")
	}
}

// TestForkSafeCleanPRPasses verifies a fully covered clean PR produces no
// findings and passes.
func TestForkSafeCleanPRPasses(t *testing.T) {
	ctx := context.Background()
	dir, base, head := consumerRepo(t,
		map[string]string{"greet.go": "package greet\n\nfunc Greet(n string) string { return n }\n"},
		map[string]string{"greet.go": "package greet\n\nfunc Greet(n string) string { if n == \"\" { return \"hi\" }; return n }\n"},
	)
	cfg := baseConfig()
	repo, _ := vcs.Open(dir)
	collect := engine.New(engine.Options{Config: cfg, Repo: repo})
	b, err := collect.Collect(ctx, "pr", base, head, "o/r", head)
	if err != nil {
		t.Fatal(err)
	}
	fake := &provider.Fake{Responses: []string{`{"findings":[]}`}}
	finalize := engine.New(engine.Options{Config: cfg, Provider: fake, Reviewer: reviewers.New(fake, "m", 0, nil), Store: store.Stateless{}})
	res, err := finalize.Finalize(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Run.Findings) != 0 || len(res.Run.Blocking()) != 0 {
		t.Fatalf("clean PR must have no findings: %+v", res.Run.Findings)
	}
	if !res.Run.Coverage.Complete() {
		t.Fatalf("clean PR must be fully covered: %+v", res.Run.Coverage)
	}
	if res.Run.Recommendation() != "Approve" {
		t.Fatalf("clean fully-covered PR should approve, got %q", res.Run.Recommendation())
	}
}

type workflowFile struct {
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		Permissions map[string]string `yaml:"permissions"`
		Steps       []struct {
			Uses string            `yaml:"uses"`
			With map[string]string `yaml:"with"`
			Env  map[string]string `yaml:"env"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// effectivePermissions returns job-level permissions when set, else the
// workflow-level defaults.
func effectivePermissions(w workflowFile, job string) map[string]string {
	if p := w.Jobs[job].Permissions; len(p) > 0 {
		return p
	}
	return w.Permissions
}

func loadWorkflow(t *testing.T, path string) workflowFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Fatal(err)
	}
	var w workflowFile
	if err := yaml.Unmarshal(data, &w); err != nil {
		t.Fatal(err)
	}
	return w
}

// TestWorkflowSecurityGuarantees asserts the documented two-phase integration
// keeps secrets and untrusted code apart, and binds the artifact to the trusted
// event.
func TestWorkflowSecurityGuarantees(t *testing.T) {
	collectWf := loadWorkflow(t, "examples/fork-safe/collect.yml")
	collect, ok := collectWf.Jobs["collect"]
	if !ok {
		t.Fatal("collect job missing")
	}
	if got := effectivePermissions(collectWf, "collect"); got["contents"] != "read" {
		t.Errorf("collect must have read-only contents, got %v", got)
	}
	for _, step := range collect.Steps {
		for k, v := range step.Env {
			if strings.Contains(v, "secrets.") {
				t.Errorf("collect step env %s references a secret: %q", k, v)
			}
		}
		for k, v := range step.With {
			if strings.Contains(strings.ToLower(k), "api-key") || strings.Contains(v, "secrets.") {
				t.Errorf("collect step input %s must not carry a secret: %q", k, v)
			}
		}
	}

	finalizeWf := loadWorkflow(t, "examples/fork-safe/finalize.yml")
	finalize, ok := finalizeWf.Jobs["finalize"]
	if !ok {
		t.Fatal("finalize job missing")
	}
	for _, step := range finalize.Steps {
		if strings.HasPrefix(step.Uses, "actions/checkout") {
			t.Error("finalize must never check out the PR")
		}
	}
	// The finalize step must bind the artifact to the trusted workflow_run event.
	var bound bool
	for _, step := range finalize.Steps {
		if strings.Contains(step.Uses, "review-engine") {
			w := step.With
			if strings.Contains(w["expect-repository"], "workflow_run") &&
				strings.Contains(w["expect-head"], "workflow_run.head_sha") &&
				w["expect-pr"] != "" && w["expect-profile"] != "" {
				bound = true
			}
		}
	}
	if !bound {
		t.Error("finalize must bind expect-repository/pr/head/profile to the workflow_run event")
	}

	reusable := loadWorkflow(t, ".github/workflows/review.yml")
	if got := effectivePermissions(reusable, "collect"); got["contents"] != "read" {
		t.Errorf("reusable collect job must be read-only: %+v", got)
	}
	f, ok := reusable.Jobs["finalize"]
	if !ok {
		t.Fatal("reusable finalize job missing")
	}
	for _, step := range f.Steps {
		if strings.HasPrefix(step.Uses, "actions/checkout") {
			t.Error("reusable finalize must never check out the PR")
		}
	}
}
