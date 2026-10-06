// Command revu is the Review Engine CLI. It runs review profiles locally and
// is the implementation behind the reusable GitHub Action.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/greadee/review-engine/internal/bundle"
	"github.com/greadee/review-engine/internal/config"
	"github.com/greadee/review-engine/internal/engine"
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
  revu version            print version

Run "revu run -h" for flags.
`)
}

type commonFlags struct {
	configPath string
	profile    string
	repoDir    string
	base       string
	head       string
	repository string
	pr         int
}

func addCommon(fs *flag.FlagSet, c *commonFlags) {
	fs.StringVar(&c.configPath, "config", "revu.yaml", "path to revu.yaml")
	fs.StringVar(&c.profile, "profile", "", "review profile (pr|issue|sprint|audit|impact)")
	fs.StringVar(&c.repoDir, "repo-dir", ".", "repository working directory")
	fs.StringVar(&c.base, "base", "origin/main", "base revision")
	fs.StringVar(&c.head, "head", "HEAD", "head revision")
	fs.StringVar(&c.repository, "repository", "", "owner/name for publishing")
	fs.IntVar(&c.pr, "pr", 0, "pull request number")
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

	cfg, err := config.Load(c.configPath)
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
		st = store.NewJSON(*stateDir)
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
	if dryRun {
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
	cfg, err := config.Load(c.configPath)
	if err != nil {
		return err
	}
	if c.profile == "" {
		c.profile = cfg.Profile
	}
	if !profiles.Valid(c.profile) {
		return fmt.Errorf("unknown profile %q", c.profile)
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
	cfg, err := config.Load(c.configPath)
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
	if len(cfg.Review.Rubric) == 0 {
		cfg.Review.Rubric = b.Rubric
	}
	prov, err := buildProvider(cfg, *dryRun)
	if err != nil {
		return err
	}
	var st store.Store = store.NewJSON(*stateDir)
	if cfg.Mode == config.ModeStateless {
		st = store.Stateless{}
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
	block := fs.Bool("block", false, "exit non-zero when criteria are unmet")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *issueNum <= 0 {
		return fmt.Errorf("--issue is required")
	}
	cfg, err := config.Load(c.configPath)
	if err != nil {
		return err
	}
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

	var st store.Store = store.NewJSON(*stateDir)
	if cfg.Mode == config.ModeStateless {
		st = store.Stateless{}
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

	unmet := 0
	for _, f := range res.Findings {
		if f.Detector == "issue.criteria" && f.Anchor != "no-criteria" {
			unmet++
		}
	}
	if *comment {
		if err := gh.Comment(ctx, owner, name, *issueNum, markdown); err != nil {
			return err
		}
	}
	if *reopen && unmet > 0 && strings.EqualFold(issue.State, "closed") {
		if err := gh.SetIssueState(ctx, owner, name, *issueNum, "open"); err != nil {
			return err
		}
		fmt.Printf("reopened issue #%d (%d unmet criteria)\n", *issueNum, unmet)
	}
	if *block && unmet > 0 {
		return fmt.Errorf("issue #%d has %d unmet acceptance criteria", *issueNum, unmet)
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
	cfg, err := config.Load(c.configPath)
	if err != nil {
		return err
	}
	if c.profile == "" {
		c.profile = cfg.Profile
	}
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

func splitRepo(s string) (string, string, error) {
	parts := strings.SplitN(strings.TrimSpace(s), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("--repository must be owner/name, got %q", s)
	}
	return parts[0], parts[1], nil
}
