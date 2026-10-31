// Package eval runs the seeded PR corpus through the review pipeline and
// computes accuracy metrics. The deterministic Oracle provider makes the
// harness hermetic in CI; a pinned real model can be substituted for an
// optional evaluation.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/greadee/review-engine/internal/config"
	"github.com/greadee/review-engine/internal/engine"
	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/provider"
	"github.com/greadee/review-engine/internal/reviewers"
	"github.com/greadee/review-engine/internal/store"
	"github.com/greadee/review-engine/internal/vcs"
)

// findingSpec is one expected finding in a fixture's manifest.
type findingSpec struct {
	File           string `json:"file"`
	Anchor         string `json:"anchor"`
	Classification string `json:"classification"`
	Severity       string `json:"severity"`
}

// Meta is a fixture manifest.
type Meta struct {
	Profile     string        `json:"profile"`
	Clean       bool          `json:"clean"`
	Adversarial bool          `json:"adversarial"`
	Notes       string        `json:"notes"`
	Expected    []findingSpec `json:"expected"`
}

// Fixture is one corpus entry.
type Fixture struct {
	Name string
	Dir  string
	Meta Meta
}

// Load reads every fixture under dir (a directory of subdirectories with
// meta.json / base / head).
func Load(dir string) ([]Fixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("eval: read corpus %s: %w", dir, err)
	}
	var out []Fixture
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fixtureDir := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(filepath.Join(fixtureDir, "meta.json"))
		if err != nil {
			return nil, fmt.Errorf("eval: fixture %s: %w", e.Name(), err)
		}
		var meta Meta
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, fmt.Errorf("eval: fixture %s meta: %w", e.Name(), err)
		}
		if meta.Profile == "" {
			meta.Profile = "pr"
		}
		out = append(out, Fixture{Name: e.Name(), Dir: fixtureDir, Meta: meta})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// FixtureResult captures one fixture's outcome.
type FixtureResult struct {
	Name             string
	Clean            bool
	Adversarial      bool
	Expected         int
	P0P1Expected     int
	Findings         int
	Detected         int
	P0P1Detected     int
	SeverityOK       int
	FalsePositives   int
	InvalidEvidence  int
	CoverageComplete bool
	Verdict          string
	Calls            int
	Latency          time.Duration
	Usage            provider.Usage
}

// Options records the exact evaluation settings and optional pricing. The
// model returned by the provider is preferred over Model when both are present.
type Options struct {
	Model              string
	MaxReasoningTokens int
	MaxBatchFiles      int
	MaxBatchBytes      int
	SupportsJSONFormat *bool
	InputCostPerMTok   float64
	OutputCostPerMTok  float64
}

// Report aggregates the corpus run.
type Report struct {
	Fixtures          []FixtureResult
	Provider          string
	Model             string
	Options           Options
	Latency           time.Duration
	Usage             provider.Usage
	InputCostPerMTok  float64
	OutputCostPerMTok float64
}

// Counts returns totals used by the threshold checks.
func (r Report) Counts() Counts {
	var c Counts
	c.Fixtures = len(r.Fixtures)
	for _, f := range r.Fixtures {
		c.Expected += f.Expected
		c.Detected += f.Detected
		c.P0P1Expected += f.P0P1Expected
		c.P0P1Detected += f.P0P1Detected
		c.SeverityOK += f.SeverityOK
		c.FalsePositives += f.FalsePositives
		c.InvalidEvidence += f.InvalidEvidence
		if f.Clean || f.Adversarial {
			c.CleanFindings += f.Findings
			if f.CoverageComplete {
				c.CleanCovered++
			}
			c.CleanFixtures++
			if f.Verdict == "Approve" {
				c.CleanApproved++
			}
		}
	}
	return c
}

// Counts is the aggregate metric set.
type Counts struct {
	Fixtures        int
	Expected        int
	Detected        int
	P0P1Expected    int
	P0P1Detected    int
	SeverityOK      int
	FalsePositives  int
	InvalidEvidence int
	CleanFixtures   int
	CleanFindings   int
	CleanCovered    int
	CleanApproved   int
}

// Recall is the overall detection rate.
func (c Counts) Recall() float64 { return ratio(c.Detected, c.Expected) }

// P0P1Recall is the detection rate for blocking-severity seeded defects.
func (c Counts) P0P1Recall() float64 { return ratio(c.P0P1Detected, c.P0P1Expected) }

// SeverityAccuracy is the share of detected defects reported at the right severity.
func (c Counts) SeverityAccuracy() float64 { return ratio(c.SeverityOK, c.Detected) }

// Precision is the share of reported findings that match a seeded defect.
func (c Counts) Precision() float64 { return ratio(c.Detected, c.Detected+c.FalsePositives) }

func ratio(a, b int) float64 {
	if b == 0 {
		return 1
	}
	return float64(a) / float64(b)
}

// Markdown renders a results table for publishing with the milestone.
func (r Report) Markdown() string {
	c := r.Counts()
	var b strings.Builder
	b.WriteString("# Seeded PR corpus results\n\n")

	b.WriteString("## Run configuration\n\n")
	b.WriteString("| Setting | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| provider | %s |\n", valueOr(r.Provider, "unknown"))
	fmt.Fprintf(&b, "| model | %s |\n", valueOr(r.Model, "(not reported)"))
	fmt.Fprintf(&b, "| temperature | 0 |\n")
	reasoning := "0 (unlimited)"
	if r.Options.MaxReasoningTokens > 0 {
		reasoning = fmt.Sprintf("%d", r.Options.MaxReasoningTokens)
	}
	fmt.Fprintf(&b, "| max reasoning tokens | %s |\n", reasoning)
	autoFiles, autoBytes := 25, 150_000
	if r.Options.MaxReasoningTokens > 0 {
		autoFiles, autoBytes = 8, 80_000
	}
	fmt.Fprintf(&b, "| max batch files | %s |\n", limitValue(r.Options.MaxBatchFiles, autoFiles, "files"))
	fmt.Fprintf(&b, "| max batch bytes | %s |\n", limitValue(r.Options.MaxBatchBytes, autoBytes, "bytes"))
	supports := "provider default"
	if r.Options.SupportsJSONFormat != nil {
		supports = fmt.Sprintf("%t", *r.Options.SupportsJSONFormat)
	}
	fmt.Fprintf(&b, "| supports response_format | %s |\n", supports)

	b.WriteString("\n## Results\n\n")
	b.WriteString("| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| fixtures | %d |\n", c.Fixtures)
	fmt.Fprintf(&b, "| seeded defects | %d |\n", c.Expected)
	fmt.Fprintf(&b, "| detected | %d |\n", c.Detected)
	fmt.Fprintf(&b, "| precision | %.0f%% |\n", c.Precision()*100)
	fmt.Fprintf(&b, "| recall (all) | %.0f%% |\n", c.Recall()*100)
	fmt.Fprintf(&b, "| recall (P0/P1) | %.0f%% |\n", c.P0P1Recall()*100)
	fmt.Fprintf(&b, "| severity accuracy | %.0f%% |\n", c.SeverityAccuracy()*100)
	fmt.Fprintf(&b, "| false positives | %d |\n", c.FalsePositives)
	fmt.Fprintf(&b, "| findings on clean/adversarial | %d |\n", c.CleanFindings)
	fmt.Fprintf(&b, "| invalid evidence | %d |\n", c.InvalidEvidence)
	fmt.Fprintf(&b, "| clean fixtures with full coverage | %d/%d |\n", c.CleanCovered, c.CleanFixtures)
	fmt.Fprintf(&b, "| latency | %s |\n", r.Latency.Round(time.Millisecond))
	fmt.Fprintf(&b, "| prompt tokens | %d |\n", r.Usage.PromptTokens)
	fmt.Fprintf(&b, "| completion tokens | %d |\n", r.Usage.CompletionTokens)
	fmt.Fprintf(&b, "| total tokens | %d |\n", r.Usage.TotalTokens)
	if cost, ok := r.CostUSD(); ok {
		fmt.Fprintf(&b, "| estimated cost | $%.6f |\n", cost)
	} else {
		b.WriteString("| estimated cost | n/a (set cost flags) |\n")
	}

	b.WriteString("\n| fixture | expected | found | detected | verdict | coverage | latency | tokens |\n|---|---|---|---|---|---|---|---|\n")
	for _, f := range r.Fixtures {
		cov := "complete"
		if !f.CoverageComplete {
			cov = "INCOMPLETE"
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %s | %s | %s | %d |\n",
			f.Name, f.Expected, f.Findings, f.Detected, f.Verdict, cov,
			f.Latency.Round(time.Millisecond), f.Usage.TotalTokens)
	}
	return b.String()
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func limitValue(value, automatic int, unit string) string {
	if value > 0 {
		return fmt.Sprintf("%d %s", value, unit)
	}
	return fmt.Sprintf("automatic (%d %s)", automatic, unit)
}

// CostUSD returns the estimated provider cost when pricing is configured.
func (r Report) CostUSD() (float64, bool) {
	if r.InputCostPerMTok == 0 && r.OutputCostPerMTok == 0 {
		return 0, false
	}
	cost := float64(r.Usage.PromptTokens)/1_000_000*r.InputCostPerMTok +
		float64(r.Usage.CompletionTokens)/1_000_000*r.OutputCostPerMTok
	return cost, true
}

// Run executes every fixture through the review pipeline with the given
// provider and computes metrics. The provider is the only source of findings,
// so passing Oracle makes the run deterministic; passing a real provider runs
// the optional pinned-model evaluation.
func Run(ctx context.Context, fixtures []Fixture, prov provider.Provider) (Report, error) {
	return RunWithOptions(ctx, fixtures, prov, Options{})
}

// RunWithOptions is Run with explicit model, batching, and pricing settings.
func RunWithOptions(ctx context.Context, fixtures []Fixture, prov provider.Provider, opts Options) (Report, error) {
	report := Report{
		Provider:          prov.Name(),
		Model:             opts.Model,
		Options:           opts,
		InputCostPerMTok:  opts.InputCostPerMTok,
		OutputCostPerMTok: opts.OutputCostPerMTok,
	}
	for _, fix := range fixtures {
		recorder := &recordingProvider{inner: prov}
		res, err := runFixture(ctx, fix, recorder, opts)
		if err != nil {
			return Report{}, fmt.Errorf("eval: fixture %s: %w", fix.Name, err)
		}
		res.Calls = recorder.calls
		res.Latency = recorder.latency
		res.Usage = recorder.usage
		report.Fixtures = append(report.Fixtures, res)
		report.Latency += recorder.latency
		addUsage(&report.Usage, recorder.usage)
		if recorder.model != "" {
			report.Model = recorder.model
		}
	}
	return report, nil
}

type recordingProvider struct {
	inner   provider.Provider
	calls   int
	latency time.Duration
	usage   provider.Usage
	model   string
}

func (r *recordingProvider) Name() string { return r.inner.Name() }

func (r *recordingProvider) Complete(ctx context.Context, req provider.Request) (provider.Response, error) {
	start := time.Now()
	resp, err := r.inner.Complete(ctx, req)
	r.calls++
	r.latency += time.Since(start)
	if resp.Model != "" {
		r.model = resp.Model
	}
	addUsage(&r.usage, resp.Usage)
	return resp, err
}

func addUsage(dst *provider.Usage, src provider.Usage) {
	dst.PromptTokens += src.PromptTokens
	dst.CompletionTokens += src.CompletionTokens
	dst.TotalTokens += src.TotalTokens
}

func runFixture(ctx context.Context, fix Fixture, prov provider.Provider, opts Options) (FixtureResult, error) {
	dir, err := os.MkdirTemp("", "revu-eval-")
	if err != nil {
		return FixtureResult{}, err
	}
	defer os.RemoveAll(dir)

	base, head, err := buildRepo(fix, dir)
	if err != nil {
		return FixtureResult{}, err
	}
	headFiles, err := treeIndex(filepath.Join(fix.Dir, "head"))
	if err != nil {
		return FixtureResult{}, err
	}

	cfg := config.Default()
	cfg.Mode = config.ModeStateless
	cfg.Review.Static = false
	cfg.Review.Semantic = true
	cfg.Review.MaxFiles = 10_000
	cfg.Review.MaxFileBytes = 10_000_000
	cfg.Review.CoverageThreshold = 1.0

	repo, err := vcs.Open(dir)
	if err != nil {
		return FixtureResult{}, err
	}
	model := opts.Model
	if model == "" {
		model = "eval"
	}
	eng := engine.New(engine.Options{
		Config:   cfg,
		Repo:     repo,
		Provider: prov,
		Reviewer: reviewers.NewWithLimits(prov, model, opts.MaxReasoningTokens, opts.SupportsJSONFormat, opts.MaxBatchFiles, opts.MaxBatchBytes),
		Store:    store.Stateless{},
	})
	res, err := eng.RunRange(ctx, fix.Meta.Profile, base, head, "eval/"+fix.Name, "HEAD")
	if err != nil {
		return FixtureResult{}, err
	}

	expected := map[string]findingSpec{}
	for _, e := range fix.Meta.Expected {
		expected[e.Anchor] = e
	}
	out := FixtureResult{
		Name:             fix.Name,
		Clean:            fix.Meta.Clean,
		Adversarial:      fix.Meta.Adversarial,
		Expected:         len(fix.Meta.Expected),
		Findings:         len(res.Run.Findings),
		CoverageComplete: res.Run.Coverage.Complete(),
		Verdict:          res.Run.Recommendation(),
	}
	seen := map[string]bool{}
	for _, f := range res.Run.Findings {
		if !validEvidence(f, headFiles) {
			out.InvalidEvidence++
		}
		e, ok := expected[f.Anchor]
		if !ok || seen[f.Anchor] {
			out.FalsePositives++
			continue
		}
		seen[f.Anchor] = true
		out.Detected++
		if string(f.Severity) == e.Severity {
			out.SeverityOK++
		}
		if e.Severity == "P0" || e.Severity == "P1" {
			out.P0P1Detected++
		}
	}
	for _, e := range fix.Meta.Expected {
		if e.Severity == "P0" || e.Severity == "P1" {
			out.P0P1Expected++
		}
	}
	return out, nil
}

func validEvidence(f findings.Finding, files map[string]string) bool {
	key := strings.TrimPrefix(strings.ReplaceAll(f.Evidence.File, "\\", "/"), "./")
	content, ok := files[key]
	if !ok {
		return false
	}
	if f.Evidence.Line > 0 && f.Evidence.Line > strings.Count(content, "\n")+1 {
		return false
	}
	return true
}

func treeIndex(root string) (map[string]string, error) {
	idx := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		idx[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if os.IsNotExist(err) {
		return idx, nil
	}
	return idx, err
}

func buildRepo(fix Fixture, dest string) (base, head string, err error) {
	if err := git(dest, "init", "-q"); err != nil {
		return "", "", err
	}
	if err := copyTree(filepath.Join(fix.Dir, "base"), dest); err != nil {
		return "", "", err
	}
	if err := git(dest, "add", "-A"); err != nil {
		return "", "", err
	}
	if err := git(dest, "commit", "-q", "-m", "base"); err != nil {
		return "", "", err
	}
	base, err = revParse(dest)
	if err != nil {
		return "", "", err
	}
	if err := copyTree(filepath.Join(fix.Dir, "head"), dest); err != nil {
		return "", "", err
	}
	if err := git(dest, "add", "-A"); err != nil {
		return "", "", err
	}
	if err := git(dest, "commit", "-q", "-m", "head"); err != nil {
		return "", "", err
	}
	head, err = revParse(dest)
	return base, head, err
}

func copyTree(src, dst string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func git(dir string, args ...string) error {
	full := append([]string{"-C", dir, "-c", "user.name=eval", "-c", "user.email=eval@example.com"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func revParse(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Oracle is a deterministic provider that reports a finding for every source
// line carrying a `revu-seed: <Classification> <Severity> <anchor>` marker. It
// ignores any other repository text, so a prompt-injection comment cannot forge
// or suppress findings.
type Oracle struct{}

// Name implements provider.Provider.
func (Oracle) Name() string { return "oracle" }

var seedMarker = regexp.MustCompile(`revu-seed:\s*([A-Za-z][A-Za-z ]*?)\s+(P[0-3])\s+(\S+)`)

// Complete implements provider.Provider.
func (Oracle) Complete(_ context.Context, req provider.Request) (provider.Response, error) {
	var user string
	for _, m := range req.Messages {
		if m.Role == "user" {
			user = m.Content
		}
	}
	var out []map[string]any
	for _, f := range reviewers.ParsePromptFiles(user) {
		for i, line := range strings.Split(f.Content, "\n") {
			m := seedMarker.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			cls := findings.Classification(strings.TrimSpace(m[1]))
			if !cls.Valid() {
				continue
			}
			out = append(out, map[string]any{
				"title":          "Seeded " + string(cls) + " defect",
				"classification": string(cls),
				"severity":       m[2],
				"file":           f.Path,
				"line":           i + 1,
				"snippet":        strings.TrimSpace(line),
				"impact":         "Seeded corpus defect.",
				"recommendation": "See the fixture notes in testdata/corpus.",
				"anchor":         m[3],
			})
		}
	}
	data, err := json.Marshal(map[string]any{"findings": out})
	if err != nil {
		return provider.Response{}, err
	}
	return provider.Response{Text: string(data), Model: "oracle"}, nil
}
