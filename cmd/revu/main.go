// Command revu is the Review Engine CLI. It runs review profiles locally and
// is the implementation behind the reusable GitHub Action.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/greadee/review-engine/internal/bundle"
	"github.com/greadee/review-engine/internal/ci"
	"github.com/greadee/review-engine/internal/config"
	"github.com/greadee/review-engine/internal/engine"
	"github.com/greadee/review-engine/internal/issues"
	"github.com/greadee/review-engine/internal/profiles"
	"github.com/greadee/review-engine/internal/provider"
	"github.com/greadee/review-engine/internal/report"
	"github.com/greadee/review-engine/internal/reviewers"
	"github.com/greadee/review-engine/internal/scope"
	"github.com/greadee/review-engine/internal/store"
	"github.com/greadee/review-engine/internal/vcs"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = run(os.Args[2:])
	case "collect":
		err = collectCmd(os.Args[2:])
	case "finalize":
		err = finalizeCmd(os.Args[2:])
	case "issue":
		err = issueReview(os.Args[2:])
	case "plan":
		err = plan(os.Args[2:])
	case "report":
		err = renderReport(os.Args[2:])
	case "providers":
		for _, name := range provider.Names() {
			fmt.Println(name)
		}
	case "init":
		err = initConfig(os.Args[2:])
	case "config":
		err = showConfig(os.Args[2:])
	case "file-issues":
		err = fileIssues(os.Args[2:])
	case "doctor":
		err = doctor(os.Args[2:])
	case "version":
		fmt.Println(version)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "revu: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `revu — general-purpose review engine

Usage:
  revu run      [flags]   collect + finalize in one process
  revu collect  [flags]   no-secret phase: produce a review bundle
  revu finalize [flags]   secret phase: review a bundle, report, publish
  revu issue    [flags]   review an issue's acceptance criteria
  revu plan     [flags]   print the resolved scope without reviewing
  revu report   [flags]   render a findings JSON file to Markdown
  revu providers          list registered model providers
  revu init               write a starter revu.yaml
  revu config             print the effective configuration
  revu doctor             check the environment and configuration
  revu file-issues        create GitHub issues from a findings file
  revu version            print version

Run "revu run -h" for flags.
`)
}

type commonFlags struct {
	configPath   string
	globalConfig string
	profile      string
	repoDir      string
	base         string
	head         string
	repository   string
	pr           int
}

func addCommon(fs *flag.FlagSet, c *commonFlags) {
	fs.StringVar(&c.configPath, "config", "revu.yaml", "path to revu.yaml")
	fs.StringVar(&c.globalConfig, "global-config", "", "path to a global revu config (defaults to $REVIEW_GLOBAL_CONFIG or the user config dir)")
	fs.StringVar(&c.profile, "profile", "", "review profile (pr|issue|sprint|audit|impact)")
	fs.StringVar(&c.repoDir, "repo-dir", ".", "repository working directory")
	fs.StringVar(&c.base, "base", "", "base revision (default: CI, else origin/main)")
	fs.StringVar(&c.head, "head", "", "head revision (default: CI, else HEAD)")
	fs.StringVar(&c.repository, "repository", "", "owner/name for publishing (default: CI)")
	fs.IntVar(&c.pr, "pr", 0, "pull request number (default: CI)")
}

// loadConfig loads configuration, honouring an explicit global config path.
func loadConfig(c commonFlags) (config.Config, error) {
	if c.globalConfig != "" {
		_ = os.Setenv("REVIEW_GLOBAL_CONFIG", c.globalConfig)
	}
	return config.Load(c.configPath)
}

// resolveCI fills empty revision flags from the CI environment.
func resolveCI(c *commonFlags) ci.Info {
	info := ci.Detect()
	if c.base == "" {
		if info.Base != "" {
			c.base = info.Base
		} else {
			c.base = "origin/main"
		}
	}
	if c.head == "" {
		if info.Head != "" {
			c.head = info.Head
		} else {
			c.head = "HEAD"
		}
	}
	if c.repository == "" {
		c.repository = info.Repository
	}
	if c.pr == 0 {
		c.pr = info.PR
	}
	return info
}

// newStore selects the tracking backend from configuration.
func newStore(cfg config.Config, stateDir string) (store.Store, func() error, error) {
	return store.Open(cfg.Store.SQLite, stateDir, cfg.Store.SQLitePath(stateDir))
}

func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var c commonFlags
	addCommon(fs, &c)
	outPath := fs.String("out", "", "write the Markdown summary to a file")
	jsonPath := fs.String("json", "", "write the findings JSON to a file")
	stateDir := fs.String("state-dir", ".review-state", "tracking state directory")
	dryRun := fs.Bool("dry-run", false, "use the fake provider and skip GitHub writes")
	noBlock := fs.Bool("no-block", false, "do not fail the run on blocking findings")
	comment := fs.Bool("comment", false, "post the summary as a PR comment")
	publish := fs.Bool("publish", false, "archive findings to the bot branch")
	providerName := fs.String("provider", "", "override provider name")
	model := fs.String("model", "", "override model id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolveCI(&c)

	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	if c.profile == "" {
		c.profile = cfg.Profile
	}
	if !profiles.Valid(c.profile) {
		return fmt.Errorf("unknown profile %q (known: %s)", c.profile, strings.Join(profiles.All(), ", "))
	}
	cfg.Profile = c.profile
	cfg.ApplyProfile(c.profile)
	if *providerName != "" {
		cfg.Provider.Name = *providerName
	}
	if *model != "" {
		cfg.Provider.Model = *model
	}
	// Per-profile default rubric when the config did not set one explicitly.
	if len(cfg.Review.Rubric) == 0 {
		cfg.Review.Rubric = profiles.Rubric(c.profile)
	}

	repo, err := vcs.Open(c.repoDir)
	if err != nil {
		return err
	}

	prov, err := buildProvider(cfg, *dryRun)
	if err != nil {
		return err
	}
	reviewer := reviewers.New(prov, cfg.Provider.Model)

	var st store.Store
	if cfg.Mode == config.ModeStateless {
		st = store.Stateless{}
	} else {
		opened, closeStore, err := newStore(cfg, *stateDir)
		if err != nil {
			return err
		}
		defer func() { _ = closeStore() }()
		st = opened
	}

	eng := engine.New(engine.Options{
		Config:   cfg,
		Repo:     repo,
		Provider: prov,
		Reviewer: reviewer,
		Store:    st,
	})

	res, err := eng.RunRange(context.Background(), c.profile, c.base, c.head, c.repository, c.head)
	if err != nil {
		return err
	}

	markdown := report.ReviewMarkdown(res.Run)
	if cfg.Profile == profiles.Audit || cfg.Profile == profiles.Sprint {
		markdown = report.AuditMarkdown(res.Run)
	}
	fmt.Print(markdown)

	if *outPath != "" {
		if err := os.WriteFile(*outPath, []byte(markdown), 0o644); err != nil {
			return err
		}
	}
	if *jsonPath != "" {
		data, err := report.JSON(res.Run)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*jsonPath, append(data, '\n'), 0o644); err != nil {
			return err
		}
	}

	if (*publish || *comment) && !*dryRun {
		owner, name, err := splitRepo(c.repository)
		if err != nil {
			return err
		}
		gh := vcs.NewGitHub("")
		if *publish {
			url, err := eng.Publish(context.Background(), gh, owner, name, res)
			if err != nil {
				return err
			}
			res.Run.ArchiveURL = url
			fmt.Printf("\narchived findings: %s\n", url)
		}
		if *comment && c.pr > 0 {
			body := report.ReviewMarkdown(res.Run)
			if res.Run.ArchiveURL != "" {
				body += fmt.Sprintf("\nFull findings: %s\n", res.Run.ArchiveURL)
			}
			if err := gh.Comment(context.Background(), owner, name, c.pr, body); err != nil {
				return err
			}
			fmt.Printf("posted comment on PR #%d\n", c.pr)
		}
	}

	if !*noBlock && len(res.Run.Blocking()) > 0 {
		return fmt.Errorf("%d blocking finding(s) at or above %s", len(res.Run.Blocking()), res.Run.BlockThreshold)
	}
	return nil
}

func buildProvider(cfg config.Config, dryRun bool) (provider.Provider, error) {
	if dryRun || !cfg.Review.Semantic {
		return &provider.Fake{Responses: []string{`{"findings":[]}`}}, nil
	}
	if cfg.Provider.Name == "fake" {
		return &provider.Fake{Responses: []string{`{"findings":[]}`}}, nil
	}
	if cfg.Provider.APIKey() == "" {
		return nil, fmt.Errorf("no API key: set %s (or use --dry-run)", defaultKeyEnv(cfg))
	}
	return provider.New(cfg.Provider.Name, provider.Config{
		BaseURL: cfg.Provider.BaseURL,
		APIKey:  cfg.Provider.APIKey(),
		Model:   cfg.Provider.Model,
	})
}

func defaultKeyEnv(cfg config.Config) string {
	if cfg.Provider.APIKeyEnv != "" {
		return cfg.Provider.APIKeyEnv
	}
	return "REVIEW_PROVIDER_API_KEY"
}

func collectCmd(args []string) error {
	fs := flag.NewFlagSet("collect", flag.ContinueOnError)
	var c commonFlags
	addCommon(fs, &c)
	out := fs.String("out", "bundle.json", "output bundle path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolveCI(&c)
	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	if c.profile == "" {
		c.profile = cfg.Profile
	}
	if !profiles.Valid(c.profile) {
		return fmt.Errorf("unknown profile %q", c.profile)
	}
	cfg.ApplyProfile(c.profile)
	if len(cfg.Review.Rubric) == 0 {
		cfg.Review.Rubric = profiles.Rubric(c.profile)
	}
	repo, err := vcs.Open(c.repoDir)
	if err != nil {
		return err
	}
	// Collect never needs a provider or tracker: it must not touch secrets.
	eng := engine.New(engine.Options{Config: cfg, Repo: repo})
	b, err := eng.Collect(context.Background(), c.profile, c.base, c.head, c.repository, c.head)
	if err != nil {
		return err
	}
	if err := bundle.Save(*out, b); err != nil {
		return err
	}
	fmt.Printf("wrote %s (profile=%s, %d files, %d static findings)\n", *out, b.Profile, len(b.Files), len(b.StaticFindings))
	return nil
}

func finalizeCmd(args []string) error {
	fs := flag.NewFlagSet("finalize", flag.ContinueOnError)
	var c commonFlags
	addCommon(fs, &c)
	bundlePath := fs.String("bundle", "bundle.json", "input bundle path")
	outPath := fs.String("out", "", "write the Markdown summary to a file")
	jsonPath := fs.String("json", "", "write the findings JSON to a file")
	stateDir := fs.String("state-dir", ".review-state", "tracking state directory")
	dryRun := fs.Bool("dry-run", false, "use the fake provider and skip GitHub writes")
	noBlock := fs.Bool("no-block", false, "do not fail on blocking findings")
	comment := fs.Bool("comment", false, "post the summary as a PR comment")
	publish := fs.Bool("publish", false, "archive findings to the bot branch")
	providerName := fs.String("provider", "", "override provider name")
	model := fs.String("model", "", "override model id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolveCI(&c)
	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	if *providerName != "" {
		cfg.Provider.Name = *providerName
	}
	if *model != "" {
		cfg.Provider.Model = *model
	}
	b, err := bundle.Load(*bundlePath)
	if err != nil {
		return err
	}
	cfg.ApplyProfile(b.Profile)
	if len(cfg.Review.Rubric) == 0 {
		cfg.Review.Rubric = b.Rubric
	}
	if len(cfg.Review.Rubric) == 0 {
		cfg.Review.Rubric = profiles.Rubric(b.Profile)
	}
	prov, err := buildProvider(cfg, *dryRun)
	if err != nil {
		return err
	}
	var st store.Store
	if cfg.Mode == config.ModeStateless {
		st = store.Stateless{}
	} else {
		opened, closeStore, err := newStore(cfg, *stateDir)
		if err != nil {
			return err
		}
		defer func() { _ = closeStore() }()
		st = opened
	}
	eng := engine.New(engine.Options{
		Config:   cfg,
		Provider: prov,
		Reviewer: reviewers.New(prov, cfg.Provider.Model),
		Store:    st,
	})
	res, err := eng.Finalize(context.Background(), b)
	if err != nil {
		return err
	}

	markdown := report.ReviewMarkdown(res.Run)
	if b.Profile == profiles.Audit || b.Profile == profiles.Sprint {
		markdown = report.AuditMarkdown(res.Run)
	}
	fmt.Print(markdown)
	if *outPath != "" {
		if err := os.WriteFile(*outPath, []byte(markdown), 0o644); err != nil {
			return err
		}
	}
	if *jsonPath != "" {
		data, err := report.JSON(res.Run)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*jsonPath, append(data, '\n'), 0o644); err != nil {
			return err
		}
	}

	if (*publish || *comment) && !*dryRun {
		repoRef := c.repository
		if repoRef == "" {
			repoRef = b.Repository
		}
		owner, name, err := splitRepo(repoRef)
		if err != nil {
			return err
		}
		gh := vcs.NewGitHub("")
		if *publish {
			url, err := eng.Publish(context.Background(), gh, owner, name, res)
			if err != nil {
				return err
			}
			res.Run.ArchiveURL = url
			fmt.Printf("\narchived findings: %s\n", url)
		}
		if *comment && c.pr > 0 {
			body := report.ReviewMarkdown(res.Run)
			if res.Run.ArchiveURL != "" {
				body += fmt.Sprintf("\nFull findings: %s\n", res.Run.ArchiveURL)
			}
			if err := gh.Comment(context.Background(), owner, name, c.pr, body); err != nil {
				return err
			}
			fmt.Printf("posted comment on PR #%d\n", c.pr)
		}
	}

	if !*noBlock && len(res.Run.Blocking()) > 0 {
		return fmt.Errorf("%d blocking finding(s) at or above %s", len(res.Run.Blocking()), res.Run.BlockThreshold)
	}
	return nil
}

func issueReview(args []string) error {
	fs := flag.NewFlagSet("issue", flag.ContinueOnError)
	var c commonFlags
	addCommon(fs, &c)
	issueNum := fs.Int("issue", 0, "issue number to review")
	stateDir := fs.String("state-dir", ".review-state", "tracking state directory")
	outPath := fs.String("out", "", "write the Markdown summary to a file")
	comment := fs.Bool("comment", false, "post the summary as an issue comment")
	reopen := fs.Bool("reopen", false, "reopen a closed issue with unmet criteria")
	closeIssue := fs.Bool("close", false, "close an open issue when every criterion is met")
	block := fs.Bool("block", false, "exit non-zero when criteria are unmet")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolveCI(&c)
	if *issueNum <= 0 {
		return fmt.Errorf("--issue is required")
	}
	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	cfg.ApplyProfile(profiles.Issue)
	doClose := *closeIssue || cfg.IssueReview.AutoClose
	doReopen := *reopen || cfg.IssueReview.AutoReopen
	owner, name, err := splitRepo(c.repository)
	if err != nil {
		return err
	}
	ctx := context.Background()
	gh := vcs.NewGitHub("")
	issue, err := gh.GetIssue(ctx, owner, name, *issueNum)
	if err != nil {
		return err
	}

	var st store.Store
	if cfg.Mode == config.ModeStateless {
		st = store.Stateless{}
	} else {
		opened, closeStore, err := newStore(cfg, *stateDir)
		if err != nil {
			return err
		}
		defer func() { _ = closeStore() }()
		st = opened
	}
	eng := engine.New(engine.Options{Config: cfg, Store: st})
	res, err := eng.RunIssue(ctx, issue)
	if err != nil {
		return err
	}

	markdown := report.ReviewMarkdown(res.Run)
	fmt.Print(markdown)
	if *outPath != "" {
		if err := os.WriteFile(*outPath, []byte(markdown), 0o644); err != nil {
			return err
		}
	}

	verdict := issues.Evaluate(issue)
	closed := strings.EqualFold(issue.State, "closed")
	if *comment {
		if err := gh.Comment(ctx, owner, name, *issueNum, markdown); err != nil {
			return err
		}
	}
	// Good to go: close the issue when every criterion is met.
	if verdict.GoodToGo() && doClose && !closed {
		if err := gh.SetIssueState(ctx, owner, name, *issueNum, "closed"); err != nil {
			return err
		}
		fmt.Printf("closed issue #%d (%d/%d criteria met)\n", *issueNum, verdict.Met, verdict.Criteria)
	}
	// Not done: keep the issue open (reopening a closed one when configured).
	if verdict.Unmet > 0 {
		if doReopen && closed {
			if err := gh.SetIssueState(ctx, owner, name, *issueNum, "open"); err != nil {
				return err
			}
			fmt.Printf("reopened issue #%d (%d unmet criteria)\n", *issueNum, verdict.Unmet)
		}
	}
	// Close gate: fail the run (e.g. as a required check) when work is unmet.
	if *block && verdict.Unmet > 0 {
		return fmt.Errorf("issue #%d has %d unmet acceptance criteria", *issueNum, verdict.Unmet)
	}
	if *block && !verdict.HasCriteria {
		return fmt.Errorf("issue #%d has no acceptance criteria to verify", *issueNum)
	}
	return nil
}

func plan(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	var c commonFlags
	addCommon(fs, &c)
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolveCI(&c)
	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	if c.profile == "" {
		c.profile = cfg.Profile
	}
	cfg.ApplyProfile(c.profile)
	repo, err := vcs.Open(c.repoDir)
	if err != nil {
		return err
	}
	changes, err := repo.Changes(c.base, c.head)
	if err != nil {
		return err
	}
	sc := scope.Resolve(c.profile, changes, cfg.Ignore)
	fmt.Printf("profile: %s\nbase: %s\nhead: %s\nfiles: %d\n", c.profile, c.base, c.head, len(sc.Files))
	for _, f := range sc.Files {
		fmt.Printf("  %s\n", f)
	}
	return nil
}

func renderReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	in := fs.String("in", "", "findings JSON file")
	out := fs.String("out", "", "output Markdown file (default stdout)")
	audit := fs.Bool("audit", false, "render the repository-audit template")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" {
		return fmt.Errorf("--in is required")
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	var r report.Run
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	if r.Generated.IsZero() {
		r.Generated = time.Now().UTC()
	}
	var md string
	if *audit {
		md = report.AuditMarkdown(r)
	} else {
		md = report.ReviewMarkdown(r)
	}
	if *out != "" {
		return os.WriteFile(*out, []byte(md), 0o644)
	}
	fmt.Print(md)
	return nil
}

func initConfig(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	path := fs.String("path", "revu.yaml", "path to write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*path); err == nil {
		return fmt.Errorf("%s already exists", *path)
	}
	if err := os.WriteFile(*path, []byte(starterConfig), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", *path)
	return nil
}

const starterConfig = `# revu configuration — all fields are optional.
profile: pr          # pr | issue | sprint | audit | impact
mode: tracking       # tracking | stateless

provider:
  name: openai-compatible
  baseUrl: https://api.openai.com/v1
  model: gpt-4o-mini
  apiKeyEnv: REVIEW_PROVIDER_API_KEY

review:
  static: true
  semantic: true
  maxFiles: 60
  maxFileBytes: 200000
  blockThreshold: P1

archive:
  enabled: true
  branch: review-artifacts
  dir: runs

store:
  sqlite: false
  path: ""

profiles: {}

issueReview:
  commentOnly: true
  autoClose: false
  autoReopen: false
  blockMerge: false

ignore:
  - "vendor/**"
`

func fileIssues(args []string) error {
	fs := flag.NewFlagSet("file-issues", flag.ContinueOnError)
	var c commonFlags
	addCommon(fs, &c)
	in := fs.String("in", "findings.json", "findings JSON file")
	group := fs.String("group", "detector", "grouping: detector|file|severity")
	label := fs.String("label", "", "extra label to apply (in addition to revu and severity)")
	max := fs.Int("max", 10, "maximum number of issues to create")
	dryRun := fs.Bool("dry-run", false, "print the issues without creating them")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolveCI(&c)
	data, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	var r report.Run
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	groups := report.GroupFindings(r.Findings, *group)
	if *max > 0 && len(groups) > *max {
		groups = groups[:*max]
	}
	owner, name, err := splitRepo(c.repository)
	if err != nil {
		return err
	}
	ctx := context.Background()
	gh := vcs.NewGitHub("")
	created := 0
	for _, g := range groups {
		labels := g.Labels
		if *label != "" {
			labels = append([]string{*label}, labels...)
		}
		if *dryRun {
			fmt.Printf("[dry-run] %s (labels=%v, %d findings)\n", g.Title, labels, len(g.Findings))
			continue
		}
		num, err := gh.CreateIssue(ctx, owner, name, g.Title, g.Body, labels)
		if err != nil {
			return err
		}
		fmt.Printf("created issue #%d: %s\n", num, g.Title)
		created++
	}
	if *dryRun {
		fmt.Printf("%d group(s) would be filed\n", len(groups))
	} else {
		fmt.Printf("%d issue(s) created\n", created)
	}
	return nil
}

func showConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	var c commonFlags
	addCommon(fs, &c)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	if c.profile != "" {
		cfg.ApplyProfile(c.profile)
	}
	data, err := config.Marshal(cfg)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

func doctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	var c commonFlags
	addCommon(fs, &c)
	stateDir := fs.String("state-dir", ".review-state", "tracking state directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	info := resolveCI(&c)
	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}

	fmt.Println("revu doctor")
	fmt.Printf("  global config: %s\n", config.GlobalPath())
	fmt.Printf("  local config:  %s\n", c.configPath)
	fmt.Printf("  profile:       %s\n", firstNonEmpty(c.profile, cfg.Profile))
	fmt.Printf("  mode:          %s\n", cfg.Mode)

	tools := []struct {
		name     string
		optional bool
	}{
		{"git", false}, {"go", false}, {"gofmt", false},
		{"ruff", true}, {"python3", true}, {"tsc", true},
	}
	for _, t := range tools {
		if _, err := exec.LookPath(t.name); err == nil {
			okf("tool %s", t.name)
		} else if t.optional {
			fmt.Printf("  [skip] tool %s not found (optional)\n", t.name)
		} else {
			warnf("tool %s not found", t.name)
		}
	}

	if _, err := vcs.Open(c.repoDir); err == nil {
		okf("git repository at %s (%s..%s)", c.repoDir, c.base, c.head)
	} else {
		warnf("git repository: %v", err)
	}

	if cfg.Review.Semantic {
		if cfg.Provider.APIKey() != "" {
			okf("provider API key via %s", defaultKeyEnv(cfg))
		} else {
			warnf("provider API key missing: set %s or use --dry-run", defaultKeyEnv(cfg))
		}
	} else {
		okf("semantic review disabled (static only)")
	}

	if cfg.Store.SQLite {
		okf("store: sqlite at %s", cfg.Store.SQLitePath(*stateDir))
	} else {
		okf("store: json at %s (sqlite is opt-in)", *stateDir)
	}

	if info.Detected {
		okf("CI: %s repository=%s base=%s head=%s pr=%d", info.Provider, info.Repository, info.Base, info.Head, info.PR)
	} else {
		fmt.Println("  [skip] CI not detected")
	}
	return nil
}

func okf(format string, a ...any)   { fmt.Printf("  [ok]   "+format+"\n", a...) }
func warnf(format string, a ...any) { fmt.Printf("  [warn] "+format+"\n", a...) }

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func splitRepo(s string) (string, string, error) {
	parts := strings.SplitN(strings.TrimSpace(s), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("--repository must be owner/name, got %q", s)
	}
	return parts[0], parts[1], nil
}
