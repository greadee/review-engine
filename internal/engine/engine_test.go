package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greadee/review-engine/internal/bundle"
	"github.com/greadee/review-engine/internal/config"
	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/provider"
	"github.com/greadee/review-engine/internal/report"
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
		Reviewer: reviewers.New(fake, "model", 0),
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

func TestPersistPolicy(t *testing.T) {
	mk := func(sev findings.Severity) []findings.Finding {
		return []findings.Finding{{Severity: sev, Classification: findings.Bug}}
	}
	cases := []struct {
		mode, risk string
		fs         []findings.Finding
		want       bool
	}{
		{"always", "P2", nil, true},
		{"", "P2", nil, true},
		{"on-findings", "P2", nil, false},
		{"on-findings", "P2", mk(findings.P3), true},
		{"on-risk", "P1", mk(findings.P2), false},
		{"on-risk", "P1", mk(findings.P1), true},
		{"on-risk", "P1", mk(findings.P0), true},
		{"never", "P2", mk(findings.P0), false},
	}
	for _, tc := range cases {
		cfg := config.Default()
		cfg.Persist.Mode = tc.mode
		cfg.Persist.Risk = tc.risk
		if got := Persist(cfg, tc.fs); got != tc.want {
			t.Errorf("Persist(%q,%q)=%v, want %v", tc.mode, tc.risk, got, tc.want)
		}
	}
}

func TestPublishLocal(t *testing.T) {
	cfg := config.Default()
	eng := New(Options{Config: cfg, Store: store.Stateless{}})
	dir := t.TempDir()
	res := Result{Run: report.Run{
		ID:       "pr@abc",
		Profile:  "pr",
		Findings: []findings.Finding{{ID: "x", Title: "t", Severity: findings.P2, Classification: findings.Bug}},
	}}
	path, err := eng.PublishLocal(dir, "aa", res)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "aa", "runs", "pr@abc.md")
	if path != want {
		t.Fatalf("path = %s, want %s", path, want)
	}
	for _, f := range []string{"pr@abc.md", "pr@abc.json"} {
		if _, err := os.Stat(filepath.Join(dir, "aa", "runs", f)); err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
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
	eng := New(Options{Config: cfg, Repo: repo, Provider: fake, Reviewer: reviewers.New(fake, "m", 0), Store: store.Stateless{}})

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

func staticOnlyEngine(t *testing.T, cfg config.Config) *Engine {
	t.Helper()
	cfg.Review.Static = false
	cfg.Review.Semantic = false
	return New(Options{Config: cfg, Store: store.Stateless{}})
}

// TestFinalizeMarksUnverifiedEvidence ensures a bundle cannot make a finding
// block unless it cites a file and line that exist in the reviewed source.
func TestFinalizeMarksUnverifiedEvidence(t *testing.T) {
	eng := staticOnlyEngine(t, config.Default())
	b := bundle.Bundle{
		Version: bundle.Version,
		Profile: "pr",
		RunID:   "pr@abc",
		Head:    "abc",
		Files:   []reviewers.FileContent{{Path: "a.go", Content: "package a\n\nvar X = 1\n"}},
		StaticFindings: []findings.Finding{
			{Title: "real", Severity: findings.P0, Classification: findings.Bug, Detector: "d",
				Evidence: findings.Evidence{File: "a.go", Line: 3, Snippet: "var X = 1"}},
			{Title: "forged", Severity: findings.P0, Classification: findings.Bug, Detector: "d2",
				Evidence: findings.Evidence{File: "evil.go", Line: 1}},
			{Title: "out of range", Severity: findings.P0, Classification: findings.Bug, Detector: "d3",
				Evidence: findings.Evidence{File: "a.go", Line: 9999}},
		},
	}
	res, err := eng.Finalize(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	blocking := res.Run.Blocking()
	if len(blocking) != 1 || blocking[0].Title != "real" {
		t.Fatalf("only the verified finding may block, got %+v", blocking)
	}
	unverified := 0
	for _, f := range res.Findings {
		if f.Unverified {
			unverified++
		}
	}
	if unverified != 2 {
		t.Fatalf("expected 2 unverified findings, got %d", unverified)
	}
	if len(res.Run.NonBlocking()) != 2 {
		t.Fatalf("unverified findings should surface as non-blocking: %+v", res.Run.NonBlocking())
	}
}

// TestFinalizeIgnoresBundleThreshold ensures the bundle cannot loosen the gate.
func TestFinalizeIgnoresBundleThreshold(t *testing.T) {
	cfg := config.Default()
	cfg.Review.BlockThreshold = "P1"
	eng := staticOnlyEngine(t, cfg)
	b := bundle.Bundle{
		Version:        bundle.Version,
		Profile:        "pr",
		RunID:          "pr@abc",
		Head:           "abc",
		BlockThreshold: "P3", // attacker claims a permissive gate
		Files:          []reviewers.FileContent{{Path: "a.go", Content: "package a\nvar X = 1\n"}},
		StaticFindings: []findings.Finding{{
			Title: "p2", Severity: findings.P2, Classification: findings.Bug, Detector: "d",
			Evidence: findings.Evidence{File: "a.go", Line: 2, Snippet: "var X = 1"},
		}},
	}
	res, err := eng.Finalize(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if res.Run.BlockThreshold != findings.P1 {
		t.Fatalf("trusted threshold must win, got %s", res.Run.BlockThreshold)
	}
	if len(res.Run.Blocking()) != 0 {
		t.Fatalf("P2 must not block at a trusted P1 gate: %+v", res.Run.Blocking())
	}
}

// TestFinalizeRejectsInvalidBundle rejects a bundle that fails schema checks.
func TestFinalizeRejectsInvalidBundle(t *testing.T) {
	eng := staticOnlyEngine(t, config.Default())
	for name, b := range map[string]bundle.Bundle{
		"version": {Version: 99, Profile: "pr", RunID: "pr@x"},
		"profile": {Version: bundle.Version, Profile: "root", RunID: "pr@x"},
		"finding": {Version: bundle.Version, Profile: "pr", RunID: "pr@x", StaticFindings: []findings.Finding{{Severity: findings.P1, Classification: findings.Bug}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := eng.Finalize(context.Background(), b); err == nil {
				t.Fatalf("expected rejection of invalid bundle (%s)", name)
			}
		})
	}
}

// TestCoverageIncompleteOnLargePR is the regression test for truncated scans:
// with more changed files than maxFiles the run must record the skips and must
// not approve.
func TestCoverageIncompleteOnLargePR(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "seed.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	base := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	for i := 0; i < 61; i++ {
		writeFile(t, dir, fmt.Sprintf("pkg/f%02d.go", i), fmt.Sprintf("package pkg\n\nvar V%02d = %d\n", i, i))
	}
	// One oversized file, larger than maxFileBytes, so it is truncated.
	writeFile(t, dir, "pkg/big.go", "package pkg\n\n// "+strings.Repeat("x", 500)+"\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "head")
	head := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	cfg := config.Default()
	cfg.Review.Static = false
	cfg.Review.Semantic = true
	cfg.Review.MaxFiles = 60
	cfg.Review.MaxFileBytes = 100
	repo, _ := vcs.Open(dir)
	fake := &provider.Fake{Responses: []string{`{"findings":[]}`}}
	eng := New(Options{Config: cfg, Repo: repo, Provider: fake, Reviewer: reviewers.New(fake, "m", 0), Store: store.Stateless{}})

	res, err := eng.RunRange(context.Background(), "pr", base, head, "o/r", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	cov := res.Run.Coverage
	if cov.Selected != 62 {
		t.Fatalf("expected 62 selected files, got %+v", cov)
	}
	if cov.Skipped != 2 {
		t.Fatalf("expected 2 skipped files, got %+v", cov)
	}
	if cov.Analyzed != 60 {
		t.Fatalf("expected 60 analyzed files, got %+v", cov)
	}
	if cov.Complete() {
		t.Fatalf("coverage with skips must be incomplete: %+v", cov)
	}
	if res.Run.Recommendation() == "Approve" {
		t.Fatal("a truncated scan must never approve")
	}
	if res.Run.Recommendation() != "Incomplete — required scope was not fully reviewed" {
		t.Fatalf("unexpected verdict %q", res.Run.Recommendation())
	}
}

// TestAuditSprintProfilesIgnoreMaxFiles is the regression test for #12:
// whole-repo profiles (audit/sprint) must not hit the maxFiles cap, so they
// don't always report INCOMPLETE coverage.
func TestAuditSprintProfilesIgnoreMaxFiles(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")

	// Create 120 files (well above the default maxFiles=60).
	for i := 0; i < 120; i++ {
		writeFile(t, dir, fmt.Sprintf("pkg/f%03d.go", i), fmt.Sprintf("package pkg\n\nvar V%03d = %d\n", i, i))
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	base := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	cfg := config.Default()
	cfg.Review.Static = false
	cfg.Review.Semantic = false
	cfg.Review.MaxFiles = 60 // default cap that should NOT apply to audit/sprint
	repo, _ := vcs.Open(dir)
	eng := New(Options{Config: cfg, Repo: repo, Store: store.Stateless{}})

	// Audit profile: should analyze ALL files, not stop at 60.
	res, err := eng.RunRange(context.Background(), "audit", base, base, "o/r", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if res.Run.Coverage.Selected != 120 {
		t.Fatalf("audit profile should see all 120 files, got selected=%d", res.Run.Coverage.Selected)
	}
	if res.Run.Coverage.Skipped != 0 {
		t.Fatalf("audit profile should skip 0 files, got skipped=%d", res.Run.Coverage.Skipped)
	}
	if !res.Run.Coverage.Complete() {
		t.Fatalf("audit coverage with 120 files must be complete, got %+v", res.Run.Coverage)
	}

	// Sprint profile: same behavior.
	res2, err := eng.RunRange(context.Background(), "sprint", base, base, "o/r", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Run.Coverage.Selected != 120 {
		t.Fatalf("sprint profile should see all 120 files, got selected=%d", res2.Run.Coverage.Selected)
	}
	if res2.Run.Coverage.Skipped != 0 {
		t.Fatalf("sprint profile should skip 0 files, got skipped=%d", res2.Run.Coverage.Skipped)
	}
	if !res2.Run.Coverage.Complete() {
		t.Fatalf("sprint coverage with 120 files must be complete, got %+v", res2.Run.Coverage)
	}
}

// TestStaticBuildFindingIsVerifiedAndBlocks is the regression test for #9:
// a static analyzer finding cites a file and line, and its snippet is the
// analyzer's diagnostic message (not source text). Evidence verification must
// therefore check file/line correspondence, not snippet containment — otherwise
// a broken build is marked unverified and never blocks.
func TestStaticBuildFindingIsVerifiedAndBlocks(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	writeFile(t, dir, "go.mod", "module badgo\n\ngo 1.26\n")
	writeFile(t, dir, "bad.go", "package badgo\n\nfunc F() int { return 1 }\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	base := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	writeFile(t, dir, "bad.go", "package badgo\n\nfunc F() int { return undefinedThing() }\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "head")
	head := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	cfg := config.Default()
	cfg.Review.Static = true
	cfg.Review.Semantic = false
	cfg.Review.MaxFiles = 50
	repo, _ := vcs.Open(dir)
	eng := New(Options{Config: cfg, Repo: repo, Store: store.Stateless{}})

	res, err := eng.RunRange(context.Background(), "pr", base, head, "o/r", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var build *findings.Finding
	for i := range res.Findings {
		if res.Findings[i].Detector == "static.gobuild" {
			build = &res.Findings[i]
		}
	}
	if build == nil {
		t.Fatalf("expected a static.gobuild finding, got %+v", res.Findings)
	}
	if build.Unverified {
		t.Fatalf("a build finding citing a real file and line must be verified: %+v", *build)
	}
	if build.Severity != findings.P1 {
		t.Fatalf("build failure should be P1, got %s", build.Severity)
	}
	if len(res.Run.Blocking()) == 0 {
		t.Fatalf("a broken build must produce a blocking finding, got %+v", res.Run.NonBlocking())
	}
}
