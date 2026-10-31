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
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/greadee/review-engine/internal/bundle"
	"github.com/greadee/review-engine/internal/ci"
	"github.com/greadee/review-engine/internal/config"
	"github.com/greadee/review-engine/internal/engine"
	"github.com/greadee/review-engine/internal/eval"
	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/issues"
	"github.com/greadee/review-engine/internal/profiles"
	"github.com/greadee/review-engine/internal/provider"
	"github.com/greadee/review-engine/internal/report"
	"github.com/greadee/review-engine/internal/reviewers"
	"github.com/greadee/review-engine/internal/scope"
	"github.com/greadee/review-engine/internal/store"
	"github.com/greadee/review-engine/internal/vcs"
)

var version = "dev"

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
	case "file-issue":
		err = fileIssue(os.Args[2:])
	case "reconcile":
		err = reconcile(os.Args[2:])
	case "doctor":
		err = doctor(os.Args[2:])
	case "eval":
		err = evalCorpus(os.Args[2:])
	case "version":
		fmt.Println(currentVersion())
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

// currentVersion prefers an injected release version, then module metadata, then
// the VCS revision of a development build.
func currentVersion() string {
	if v := strings.TrimSpace(version); v != "" && v != "dev" {
		return v
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := strings.TrimSpace(info.Main.Version); v != "" && v != "(devel)" {
		return v
	}
	var revision string
	var dirty bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if revision == "" {
		return "dev"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if dirty {
		return "dev+" + revision + ".dirty"
	}
	return "dev+" + revision
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
  revu file-issue         create one findings issue from a findings file
  revu reconcile          review issues and close/keep-open via their criteria
  revu eval               run the seeded PR corpus and report accuracy
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

// loadTrustedConfig reads the review policy from the base revision. The
// working tree belongs to the pull request and must not decide what gets
// reviewed or whether static analysis runs.
func loadTrustedConfig(repo *vcs.Repo, base string) (config.Config, error) {
	data, err := repo.File(base, "revu.yaml")
	if err != nil {
		return config.LoadData(nil)
	}
	cfg, err := config.LoadData(data)
	if err != nil {
		return config.Config{}, fmt.Errorf("config: trusted policy at %s: %w", base, err)
	}
	return cfg, nil
}

// localScope derives the trusted changed-file scope from the local repository.
func localScope(repo *vcs.Repo, profile, base, head string, ignore []string) ([]string, error) {
	if profile != profiles.PR && profile != profiles.Impact {
		return nil, nil
	}
	changes, err := scope.ChangesForProfile(repo, profile, base, head)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, nil
	}
	sc := scope.Resolve(profile, changes, ignore)
	if len(sc.Files) == 0 {
		return nil, fmt.Errorf("trusted policy selected no files from %d changed file(s)", len(changes))
	}
	return sc.Files, nil
}

// githubScope derives the trusted changed-file scope from the pull request API.
func githubScope(ctx context.Context, gh *vcs.GitHub, owner, repo string, b bundle.Bundle, ignore []string) ([]string, error) {
	if b.Profile != profiles.PR && b.Profile != profiles.Impact {
		return nil, nil
	}
	if b.PR <= 0 {
		return nil, fmt.Errorf("pull request number is required to verify review scope")
	}
	files, err := gh.PullRequestFiles(ctx, owner, repo, b.PR)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	changes := make([]vcs.Change, 0, len(files))
	for _, f := range files {
		status := "M"
		if f.Status == "removed" {
			status = "D"
		}
		changes = append(changes, vcs.Change{Status: status, Path: f.Filename})
	}
	sc := scope.Resolve(b.Profile, changes, ignore)
	if len(sc.Files) == 0 {
		return nil, fmt.Errorf("trusted policy selected no files from %d changed file(s)", len(files))
	}
	return sc.Files, nil
}

// trustedBaseConfig fetches revu.yaml from the pull request's base revision,
// not from the pull-request checkout.
func trustedBaseConfig(ctx context.Context, gh *vcs.GitHub, owner, repo string, b bundle.Bundle) (config.Config, error) {
	if b.PR <= 0 {
		return config.Config{}, fmt.Errorf("pull request number is required to load the trusted policy")
	}
	pr, err := gh.PR(ctx, owner, repo, b.PR)
	if err != nil {
		return config.Config{}, err
	}
	if b.Head != "" && !strings.EqualFold(pr.Head.SHA, b.Head) && !strings.EqualFold(pr.MergeCommitSHA, b.Head) {
		return config.Config{}, fmt.Errorf("bundle head %s does not match pull request head %s (merge commit %s)", b.Head, pr.Head.SHA, pr.MergeCommitSHA)
	}
	data, found, err := gh.FileAtRef(ctx, owner, repo, "revu.yaml", pr.Base.SHA)
	if err != nil {
		return config.Config{}, err
	}
	if !found {
		data = nil
	}
	cfg, err := config.LoadData(data)
	if err != nil {
		return config.Config{}, fmt.Errorf("config: trusted policy at %s: %w", pr.Base.SHA, err)
	}
	cfg.ApplyProfile(b.Profile)
	return cfg, nil
}

// localArchiveGuard rejects archive targets that are the checked-out branch or
// the local default branch.
func localArchiveGuard(repo *vcs.Repo) engine.ArchiveGuard {
	return func(_ context.Context, branch string) error {
		if strings.EqualFold(branch, "main") || strings.EqualFold(branch, "master") {
			return fmt.Errorf("refusing to write findings to a protected branch")
		}
		if def, err := repo.DefaultBranch(); err == nil && strings.EqualFold(branch, def) {
			return fmt.Errorf("refusing to write findings to the default branch")
		}
		if current, err := repo.CurrentBranch(); err == nil && strings.EqualFold(branch, current) {
			return fmt.Errorf("refusing to write findings to the pull request branch")
		}
		return nil
	}
}

// githubArchiveGuard rejects archive targets that are the repository default
// branch or the pull request head branch.
func githubArchiveGuard(ctx context.Context, gh *vcs.GitHub, owner, repo string, pr int) engine.ArchiveGuard {
	return func(ctx context.Context, branch string) error {
		def, err := gh.DefaultBranch(ctx, owner, repo)
		if err != nil {
			return err
		}
		if strings.EqualFold(branch, def) {
			return fmt.Errorf("refusing to write findings to the default branch %q", def)
		}
		if pr > 0 {
			info, err := gh.PR(ctx, owner, repo, pr)
			if err != nil {
				return err
			}
			if strings.EqualFold(branch, info.Head.Ref) {
				return fmt.Errorf("refusing to write findings to the pull request branch %q", info.Head.Ref)
			}
		}
		return nil
	}
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
func newStore(cfg config.Config, stateDir string, pr int, profile string) (store.Store, func() error, error) {
	dbPath := cfg.Store.SQLitePath(stateDir)
	if cfg.Store.SQLite && pr > 0 && strings.TrimSpace(cfg.Store.Path) != "" {
		dbPath = scopedSQLitePath(cfg.Store.Path, pr, profile)
	}
	return store.Open(cfg.Store.SQLite, stateDir, dbPath)
}

// scopedStateDir keeps lifecycle state per pull request and profile. A shared
// state file lets one PR or profile mark another's findings resolved.
func scopedStateDir(base string, pr int, profile string) string {
	if pr <= 0 {
		return base
	}
	if profile == "" {
		profile = "default"
	}
	return filepath.Join(base, fmt.Sprintf("pr-%d", pr), profile)
}

// scopedSQLitePath applies the same per-PR/profile isolation when the consumer
// pins an explicit SQLite file instead of using the state directory default.
func scopedSQLitePath(path string, pr int, profile string) string {
	path = strings.TrimSpace(path)
	if pr <= 0 || path == "" || path == ":memory:" {
		return path
	}
	if profile == "" {
		profile = "default"
	}
	ext := filepath.Ext(path)
	if ext == "" {
		return fmt.Sprintf("%s.pr-%d.%s", path, pr, profile)
	}
	return fmt.Sprintf("%s.pr-%d.%s%s", strings.TrimSuffix(path, ext), pr, profile, ext)
}

// stateScope makes the command-line PR authoritative over artifact data. A
// mismatch is rejected rather than selecting an attacker-controlled state key.
func stateScope(flagPR, bundlePR int) (int, error) {
	if flagPR > 0 && bundlePR > 0 && flagPR != bundlePR {
		return 0, fmt.Errorf("bundle pull request #%d does not match --pr #%d", bundlePR, flagPR)
	}
	if flagPR > 0 {
		return flagPR, nil
	}
	return bundlePR, nil
}

// projectName derives the local project folder name: the repository name when
// known, otherwise the base of the repository directory.
func projectName(repository, repoDir string) string {
	if repository = strings.TrimSpace(repository); repository != "" {
		parts := strings.Split(repository, "/")
		return parts[len(parts)-1]
	}
	abs, err := filepath.Abs(repoDir)
	if err != nil {
		return "project"
	}
	return filepath.Base(abs)
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
	local := fs.Bool("local", false, "write findings to a local project folder; never touches GitHub")
	localDir := fs.String("local-dir", "", "local storage root (default: config local.dir)")
	quiet := fs.Bool("quiet", false, "suppress the rendered summary on stdout")
	persist := fs.String("persist", "", "when to write findings: always|on-findings|on-risk|never")
	risk := fs.String("risk", "", "severity threshold for --persist on-risk (P0..P3)")
	providerName := fs.String("provider", "", "override provider name")
	model := fs.String("model", "", "override model id")
	staticFlag := fs.String("static", "", "override static analysis (true|false)")
	semanticFlag := fs.String("semantic", "", "override semantic review (true|false)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolveCI(&c)

	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	repo, err := vcs.Open(c.repoDir)
	if err != nil {
		return err
	}
	trusted, err := loadTrustedConfig(repo, c.base)
	if err != nil {
		return err
	}
	if c.profile == "" {
		c.profile = trusted.Profile
	}
	if !profiles.Valid(c.profile) {
		return fmt.Errorf("unknown profile %q (known: %s)", c.profile, strings.Join(profiles.All(), ", "))
	}
	cfg.Profile = c.profile
	trusted.ApplyProfile(c.profile)
	cfg.ApplyProfile(c.profile)
	if *local || *localDir != "" {
		cfg.Local.Enabled = true
	}
	if *localDir != "" {
		cfg.Local.Dir = *localDir
	}
	if *persist != "" {
		cfg.Persist.Mode = *persist
	}
	if *risk != "" {
		cfg.Persist.Risk = *risk
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
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

	// Security-sensitive policy comes from the base revision, not the PR
	// checkout. Provider, archive, and persistence settings remain caller
	// configuration.
	cfg.Ignore = trusted.Ignore
	cfg.Review.Static = trusted.Review.Static
	cfg.Review.Semantic = trusted.Review.Semantic
	cfg.Review.MaxFiles = trusted.Review.MaxFiles
	cfg.Review.MaxFileBytes = trusted.Review.MaxFileBytes
	cfg.Review.BlockThreshold = trusted.Review.BlockThreshold
	cfg.Review.CoverageThreshold = trusted.Review.CoverageThreshold
	// Batch limits are policy too, but a workflow-controlled action input or
	// environment override must win over repository configuration.
	if os.Getenv("REVIEW_MAX_BATCH_FILES") == "" {
		cfg.Review.MaxBatchFiles = trusted.Review.MaxBatchFiles
	}
	if os.Getenv("REVIEW_MAX_BATCH_BYTES") == "" {
		cfg.Review.MaxBatchBytes = trusted.Review.MaxBatchBytes
	}
	if len(trusted.Review.Rubric) > 0 {
		cfg.Review.Rubric = trusted.Review.Rubric
	}
	if *staticFlag != "" {
		v, err := strconv.ParseBool(strings.TrimSpace(*staticFlag))
		if err != nil {
			return fmt.Errorf("--static: %w", err)
		}
		cfg.Review.Static = v
	}
	if *semanticFlag != "" {
		v, err := strconv.ParseBool(strings.TrimSpace(*semanticFlag))
		if err != nil {
			return fmt.Errorf("--semantic: %w", err)
		}
		cfg.Review.Semantic = v
	}

	prov, err := buildProvider(cfg, *dryRun)
	if err != nil {
		return err
	}
	reviewer := reviewers.NewWithLimits(prov, cfg.Provider.Model, cfg.Provider.MaxReasoningTokens, cfg.Provider.SupportsJSONFormat, cfg.Review.MaxBatchFiles, cfg.Review.MaxBatchBytes)

	// Local mode stores tracking state under the project folder and never
	// touches GitHub.
	project := projectName(c.repository, c.repoDir)
	effectiveStateDir := *stateDir
	if cfg.Local.Enabled {
		effectiveStateDir = filepath.Join(cfg.Local.Dir, project)
	}
	effectiveStateDir = scopedStateDir(effectiveStateDir, c.pr, c.profile)
	var st store.Store
	if cfg.Mode == config.ModeStateless || strings.EqualFold(strings.TrimSpace(cfg.Persist.Mode), "never") || *dryRun {
		st = store.Stateless{}
	} else {
		opened, closeStore, err := newStore(cfg, effectiveStateDir, c.pr, c.profile)
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
		ScopeVerifier: func(_ context.Context, b bundle.Bundle) ([]string, error) {
			return localScope(repo, b.Profile, b.Base, b.Head, trusted.Ignore)
		},
		ArchiveGuard: localArchiveGuard(repo),
	})

	res, err := eng.RunRange(context.Background(), c.profile, c.base, c.head, c.repository, c.head)
	if err != nil {
		return err
	}

	markdown := report.ReviewMarkdown(res.Run)
	if cfg.Profile == profiles.Audit || cfg.Profile == profiles.Sprint {
		markdown = report.AuditMarkdown(res.Run)
	}
	if !*quiet {
		fmt.Print(markdown)
	}

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

	// Resolve owner/name once for publishing and commenting.
	owner, name, hasRepo := "", "", false
	if strings.TrimSpace(c.repository) != "" {
		o, n, err := splitRepo(c.repository)
		if err != nil {
			return err
		}
		owner, name, hasRepo = o, n, true
	}

	// Local store: only when the persist policy allowed writing.
	if cfg.Local.Enabled {
		if res.Persisted {
			path, err := eng.PublishLocal(cfg.Local.Dir, project, res)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "local findings: %s\n", path)
		} else {
			fmt.Fprintf(os.Stderr, "local findings: skipped (persist=%s)\n", strings.ToLower(strings.TrimSpace(cfg.Persist.Mode)))
		}
	}

	ghToken := cfg.GitHubToken
	// Remote archive: only when the persist policy allowed writing.
	if !cfg.Local.Enabled && *publish && !*dryRun && res.Persisted {
		if !hasRepo {
			return fmt.Errorf("--repository is required to publish")
		}
		gh := vcs.NewGitHub(ghToken)
		url, err := eng.Publish(context.Background(), gh, owner, name, res)
		if err != nil {
			return err
		}
		res.Run.ArchiveURL = url
		fmt.Printf("\narchived findings: %s\n", url)
	}

	// Notification is independent of persistence: publish/notify even when no
	// findings are stored.
	if *comment && c.pr > 0 && !*dryRun {
		if !hasRepo {
			return fmt.Errorf("--repository is required to comment")
		}
		gh := vcs.NewGitHub(ghToken)
		marker := fmt.Sprintf("<!-- revu:pr=%d -->", c.pr)
		if err := gh.UpsertComment(context.Background(), owner, name, c.pr, marker, report.SummaryMarkdown(res.Run)); err != nil {
			return err
		}
		fmt.Printf("posted comment on PR #%d\n", c.pr)
	}

	if !*noBlock && len(res.Run.Blocking()) > 0 {
		return fmt.Errorf("%d blocking finding(s) at or above %s", len(res.Run.Blocking()), res.Run.BlockThreshold)
	}
	if !*noBlock && !res.Run.Coverage.Complete() {
		return fmt.Errorf("review coverage incomplete (%s); lower review.coverageThreshold or raise maxFiles/maxFileBytes to accept", res.Run.Coverage)
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
	// Refuse to attach the credential to an endpoint that trusted configuration
	// has not allowlisted. A repository's revu.yaml is untrusted in the
	// secret-bearing job, so its provider.baseUrl must not redirect the key.
	if err := checkProviderEndpoint(cfg); err != nil {
		return nil, err
	}
	return provider.New(cfg.Provider.Name, provider.Config{
		BaseURL:            cfg.Provider.BaseURL,
		APIKey:             cfg.Provider.APIKey(),
		Model:              cfg.Provider.Model,
		MaxReasoningTokens: cfg.Provider.MaxReasoningTokens,
	})
}

// checkProviderEndpoint enforces the trusted provider-endpoint allowlist.
func checkProviderEndpoint(cfg config.Config) error {
	allowed := append(provider.TrustedBaseURLs(), config.GlobalProviderBaseURLs()...)
	return provider.CheckEndpoint(cfg.Provider.BaseURL, allowed)
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
	repo, err := vcs.Open(c.repoDir)
	if err != nil {
		return err
	}
	cfg, err := loadTrustedConfig(repo, c.base)
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
	// Collect never needs a provider or tracker: it must not touch secrets.
	eng := engine.New(engine.Options{Config: cfg, Repo: repo, PR: c.pr})
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
	expectRepo := fs.String("expect-repository", "", "require the bundle's repository to match")
	expectPR := fs.Int("expect-pr", 0, "require the bundle's pull request number to match")
	expectHead := fs.String("expect-head", "", "require the bundle's head revision to match")
	expectProfile := fs.String("expect-profile", "", "require the bundle's profile to match")
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
	// Bind the untrusted artifact to the trusted workflow event so a tampered or
	// replayed bundle cannot be finalized as if it described another review.
	if err := b.Verify(bundle.Trusted{
		Repository: *expectRepo,
		PR:         *expectPR,
		Head:       *expectHead,
		Profile:    *expectProfile,
	}); err != nil {
		return err
	}
	cfg.ApplyProfile(b.Profile)
	if len(cfg.Review.Rubric) == 0 {
		cfg.Review.Rubric = profiles.Rubric(b.Profile)
	}

	ctx := context.Background()
	needsScope := b.Profile == profiles.PR || b.Profile == profiles.Impact
	needsGitHub := needsScope || *publish || *comment
	var gh *vcs.GitHub
	var owner, name string
	var hasRepo bool
	if needsGitHub && !*dryRun {
		repoRef := c.repository
		if repoRef == "" {
			repoRef = b.Repository
		}
		owner, name, err = splitRepo(repoRef)
		if err != nil {
			return err
		}
		hasRepo = true
		gh = vcs.NewGitHub(cfg.GitHubToken)
	}

	var verifier engine.ScopeVerifier
	if needsScope && !*dryRun {
		trusted, err := trustedBaseConfig(ctx, gh, owner, name, b)
		if err != nil {
			return err
		}
		verifier = func(ctx context.Context, b bundle.Bundle) ([]string, error) {
			return githubScope(ctx, gh, owner, name, b, trusted.Ignore)
		}
	}
	var guard engine.ArchiveGuard
	if *publish && !*dryRun {
		guard = githubArchiveGuard(ctx, gh, owner, name, b.PR)
	}

	prov, err := buildProvider(cfg, *dryRun)
	if err != nil {
		return err
	}
	scopePR, err := stateScope(c.pr, b.PR)
	if err != nil {
		return err
	}
	var st store.Store
	if cfg.Mode == config.ModeStateless || *dryRun {
		st = store.Stateless{}
	} else {
		opened, closeStore, err := newStore(cfg, scopedStateDir(*stateDir, scopePR, b.Profile), scopePR, b.Profile)
		if err != nil {
			return err
		}
		defer func() { _ = closeStore() }()
		st = opened
	}
	eng := engine.New(engine.Options{
		Config:        cfg,
		Provider:      prov,
		Reviewer:      reviewers.NewWithLimits(prov, cfg.Provider.Model, cfg.Provider.MaxReasoningTokens, cfg.Provider.SupportsJSONFormat, cfg.Review.MaxBatchFiles, cfg.Review.MaxBatchBytes),
		Store:         st,
		ScopeVerifier: verifier,
		ArchiveGuard:  guard,
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
		if !hasRepo {
			return fmt.Errorf("--repository is required to publish or comment")
		}
		if *publish {
			url, err := eng.Publish(ctx, gh, owner, name, res)
			if err != nil {
				return err
			}
			res.Run.ArchiveURL = url
			fmt.Printf("\narchived findings: %s\n", url)
		}
		if *comment && c.pr > 0 {
			marker := fmt.Sprintf("<!-- revu:pr=%d -->", c.pr)
			if err := gh.UpsertComment(ctx, owner, name, c.pr, marker, report.SummaryMarkdown(res.Run)); err != nil {
				return err
			}
			fmt.Printf("posted comment on PR #%d\n", c.pr)
		}
	}

	if !*noBlock && len(res.Run.Blocking()) > 0 {
		return fmt.Errorf("%d blocking finding(s) at or above %s", len(res.Run.Blocking()), res.Run.BlockThreshold)
	}
	if !*noBlock && !res.Run.Coverage.Complete() {
		return fmt.Errorf("review coverage incomplete (%s); lower review.coverageThreshold or raise maxFiles/maxFileBytes to accept", res.Run.Coverage)
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
	gh := vcs.NewGitHub(cfg.GitHubToken)
	issue, err := gh.GetIssue(ctx, owner, name, *issueNum)
	if err != nil {
		return err
	}

	var st store.Store
	if cfg.Mode == config.ModeStateless {
		st = store.Stateless{}
	} else {
		opened, closeStore, err := newStore(cfg, *stateDir, 0, "")
		if err != nil {
			return err
		}
		defer func() { _ = closeStore() }()
		st = opened
	}
	repo, _ := vcs.Open(c.repoDir) // best-effort: used to read linked criteria docs
	var reader issues.DocReader
	if repo != nil {
		reader = func(p string) (string, bool) {
			data, err := repo.File("", p)
			if err != nil {
				return "", false
			}
			return string(data), true
		}
	}
	eng := engine.New(engine.Options{Config: cfg, Repo: repo, Store: st})
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

	approved := cfg.IssueReview.VerifiedLabel != "" && hasLabel(issue.Labels, cfg.IssueReview.VerifiedLabel)
	verdict := issues.EvaluateWithOptions(issue, reader, issues.Options{Approved: approved})
	closed := strings.EqualFold(issue.State, "closed")
	if *comment {
		marker := fmt.Sprintf("<!-- revu:issue=%d -->", *issueNum)
		if err := gh.UpsertComment(ctx, owner, name, *issueNum, marker, report.SummaryMarkdown(res.Run)); err != nil {
			return err
		}
	}
	// Good to go: close the issue only when every criterion is independently
	// verified (or a reviewer explicitly approved it).
	if verdict.GoodToGo() && doClose && !closed {
		if err := gh.SetIssueState(ctx, owner, name, *issueNum, "closed"); err != nil {
			return err
		}
		fmt.Printf("closed issue #%d (%d/%d criteria verified)\n", *issueNum, verdict.Verified, verdict.Criteria)
	}
	// Not done: keep the issue open (reopening a closed one when configured).
	if verdict.Unmet > 0 {
		if doReopen && closed {
			if err := gh.SetIssueState(ctx, owner, name, *issueNum, "open"); err != nil {
				return err
			}
			fmt.Printf("reopened issue #%d (%d unverified criteria)\n", *issueNum, verdict.Unmet)
		}
	}
	// Close gate: fail the run (e.g. as a required check) when work is not
	// independently verified. Checklist state alone never passes the check.
	if *block && !verdict.GoodToGo() {
		if !verdict.HasCriteria {
			return fmt.Errorf("issue #%d has no acceptance criteria to verify", *issueNum)
		}
		return fmt.Errorf("issue #%d has %d criteria without verified evidence (%d checked but unverified)", *issueNum, verdict.Unmet, verdict.Unverified)
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
	repo, err := vcs.Open(c.repoDir)
	if err != nil {
		return err
	}
	cfg, err := loadTrustedConfig(repo, c.base)
	if err != nil {
		return err
	}
	if c.profile == "" {
		c.profile = cfg.Profile
	}
	cfg.ApplyProfile(c.profile)
	changes, err := scope.ChangesForProfile(repo, c.profile, c.base, c.head)
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
	summary := fs.Bool("summary", false, "render the concise summary template")
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
	switch {
	case *audit:
		md = report.AuditMarkdown(r)
	case *summary:
		md = report.SummaryMarkdown(r)
	default:
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
  coverageThreshold: 1.0

archive:
  enabled: true
  branch: review-artifacts
  dir: runs

local:
  enabled: false      # write findings to <dir>/<project>/ without touching GitHub
  dir: .review-engine

persist:
  mode: always        # always | on-findings | on-risk | never
  risk: P2            # severity threshold used by on-risk

store:
  sqlite: false
  path: ""

profiles: {}

issueReview:
  commentOnly: true
  autoClose: false
  autoReopen: false
  blockMerge: false
  verifiedLabel: ""   # a label that counts as explicit reviewer approval

ignore:
  - "vendor/**"
`

// fileIssue creates a single consolidated findings issue. Review output is one
// issue, never a burst of issues.
func fileIssue(args []string) error {
	fs := flag.NewFlagSet("file-issue", flag.ContinueOnError)
	var c commonFlags
	addCommon(fs, &c)
	in := fs.String("in", "findings.json", "findings JSON file")
	title := fs.String("title", "", "issue title (default derived from the run)")
	group := fs.String("group", "detector", "how to section the body: detector|file|severity")
	label := fs.String("label", "", "extra label to apply")
	dryRun := fs.Bool("dry-run", false, "print the issue without creating it")
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
	if len(r.Findings) == 0 {
		fmt.Println("no findings; no issue filed")
		return nil
	}
	issueTitle := *title
	if issueTitle == "" {
		issueTitle = fmt.Sprintf("[revu] %s review findings (%d)", firstNonEmpty(r.Profile, "review"), len(r.Findings))
	}
	body := report.ConsolidatedBody(r, *group)
	labels := []string{"revu"}
	if *label != "" {
		labels = append(labels, *label)
	}

	if *dryRun {
		fmt.Printf("# %s\nlabels=%v\n\n%s\n", issueTitle, labels, body)
		return nil
	}
	owner, name, err := splitRepo(c.repository)
	if err != nil {
		return err
	}
	num, err := vcs.NewGitHub("").CreateIssue(context.Background(), owner, name, issueTitle, body, labels)
	if err != nil {
		return err
	}
	fmt.Printf("created issue #%d: %s\n", num, issueTitle)
	return nil
}

// reconcile reviews a set of issues against their acceptance criteria: it
// closes the ones that are good to go, keeps the rest open, and comments on the
// remaining ones.
func reconcile(args []string) error {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	var c commonFlags
	addCommon(fs, &c)
	numbers := fs.String("issues", "", "comma-separated issue numbers")
	findingsPath := fs.String("findings", "", "findings JSON; hold open any issue with related findings")
	comment := fs.Bool("comment", false, "comment on issues that remain open")
	closeGTG := fs.Bool("close", false, "close issues whose criteria are all met")
	block := fs.Bool("block", false, "exit non-zero when any issue is not good to go")
	dryRun := fs.Bool("dry-run", false, "print the actions without changing issues")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolveCI(&c)
	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	cfg.ApplyProfile(profiles.Issue)
	doClose := *closeGTG || cfg.IssueReview.AutoClose
	nums, err := parseNumbers(*numbers)
	if err != nil {
		return err
	}
	if len(nums) == 0 {
		return fmt.Errorf("--issues is required (e.g. --issues 23,24)")
	}
	owner, name, err := splitRepo(c.repository)
	if err != nil {
		return err
	}

	var reader issues.DocReader
	if repo, err := vcs.Open(c.repoDir); err == nil {
		reader = func(p string) (string, bool) {
			data, err := repo.File("", p)
			if err != nil {
				return "", false
			}
			return string(data), true
		}
	}

	ctx := context.Background()
	gh := vcs.NewGitHub("")

	var allFindings []findings.Finding
	if *findingsPath != "" {
		data, err := os.ReadFile(*findingsPath)
		if err != nil {
			return err
		}
		var fr report.Run
		if err := json.Unmarshal(data, &fr); err != nil {
			return err
		}
		allFindings = fr.Findings
	}

	var closedN, openN int
	for _, n := range nums {
		issue, err := gh.GetIssue(ctx, owner, name, n)
		if err != nil {
			return fmt.Errorf("issue #%d: %w", n, err)
		}
		approved := cfg.IssueReview.VerifiedLabel != "" && hasLabel(issue.Labels, cfg.IssueReview.VerifiedLabel)
		verdict := issues.EvaluateWithOptions(issue, reader, issues.Options{Approved: approved})
		isClosed := strings.EqualFold(issue.State, "closed")

		// A findings-gated issue stays open when the review found problems in
		// its area, even if its own checklist claims completion.
		if related := issues.RelatedFindings(issue, reader, allFindings); len(related) > 0 {
			if isClosed && !*dryRun {
				if err := gh.SetIssueState(ctx, owner, name, n, "open"); err != nil {
					return err
				}
				fmt.Printf("#%d reopened (%d related finding(s))\n", n, len(related))
			} else {
				fmt.Printf("#%d kept open (%d related finding(s))\n", n, len(related))
			}
			openN++
			if *comment && !*dryRun {
				body := relatedFindingsComment(issue, related)
				if err := gh.UpsertComment(ctx, owner, name, n, fmt.Sprintf("<!-- revu:reconcile=issue-%d -->", n), body); err != nil {
					return err
				}
			}
			continue
		}

		switch {
		case verdict.GoodToGo() && !isClosed:
			if doClose {
				if !*dryRun {
					if err := gh.SetIssueState(ctx, owner, name, n, "closed"); err != nil {
						return err
					}
				}
				fmt.Printf("#%d closed (%d/%d criteria verified)\n", n, verdict.Verified, verdict.Criteria)
				closedN++
			} else {
				fmt.Printf("#%d good to go (%d/%d criteria verified)\n", n, verdict.Verified, verdict.Criteria)
				openN++
			}
		case verdict.GoodToGo():
			fmt.Printf("#%d remains closed (good to go, %d/%d)\n", n, verdict.Verified, verdict.Criteria)
			closedN++
		default:
			reason := fmt.Sprintf("%d/%d criteria verified", verdict.Verified, verdict.Criteria)
			if !verdict.HasCriteria {
				reason = "no acceptance criteria found"
			}
			fmt.Printf("#%d kept open (%s)\n", n, reason)
			openN++
			if *comment && !*dryRun {
				body := reconcileComment(issue, verdict, reader)
				if err := gh.UpsertComment(ctx, owner, name, n, fmt.Sprintf("<!-- revu:reconcile=issue-%d -->", n), body); err != nil {
					return err
				}
			}
		}
	}
	fmt.Printf("reconciled %d issue(s): %d closed, %d open\n", len(nums), closedN, openN)
	if *block {
		if openN > 0 {
			return fmt.Errorf("%d issue(s) are not good to go", openN)
		}
	}
	return nil
}

func reconcileComment(issue vcs.Issue, v issues.Verdict, read issues.DocReader) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Review update** for #%d\n\n", issue.Number)
	if !v.HasCriteria {
		b.WriteString("No acceptance criteria were found in the issue or its referenced docs.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Acceptance criteria: %d/%d verified.\n\n", v.Verified, v.Criteria)
	unmet := issues.Unmet(issue, read)
	if len(unmet) > 0 {
		b.WriteString("Outstanding (no verified evidence):\n")
		for _, c := range unmet {
			box := " "
			if c.Done {
				box = "x"
			}
			fmt.Fprintf(&b, "- [%s] %s\n", box, c.Text)
		}
	}
	b.WriteString("\n_Automated by review-engine._\n")
	return b.String()
}

func relatedFindingsComment(issue vcs.Issue, related []findings.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Review update** for #%d\n\n", issue.Number)
	fmt.Fprintf(&b, "The automated review found %d finding(s) in this issue's area, so it stays open pending fixes:\n\n", len(related))
	const max = 15
	for i, f := range related {
		if i >= max {
			fmt.Fprintf(&b, "- _…and %d more_\n", len(related)-max)
			break
		}
		loc := f.Evidence.File
		if f.Evidence.Line > 0 {
			loc = fmt.Sprintf("%s:%d", loc, f.Evidence.Line)
		}
		fmt.Fprintf(&b, "- **[%s] %s** — `%s`\n", f.Severity, f.Title, loc)
	}
	b.WriteString("\n_Automated by review-engine._\n")
	return b.String()
}

func parseNumbers(s string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid issue number %q", part)
		}
		out = append(out, n)
	}
	return out, nil
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
	data, err := config.Marshal(cfg.Redacted())
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

// evalCorpus runs the seeded PR corpus through the review pipeline. The default
// oracle provider is deterministic (no network); a pinned model can be selected
// for the optional evaluation.
func evalCorpus(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	corpus := fs.String("corpus", "testdata/corpus", "corpus directory")
	out := fs.String("out", "", "write the results Markdown to a file")
	providerName := fs.String("provider", "oracle", "provider: oracle | fake | openai-compatible")
	model := fs.String("model", "", "model id for a provider-backed evaluation")
	maxReasoning := fs.Int("max-reasoning-tokens", 0, "max reasoning tokens for the pinned model")
	maxBatchFiles := fs.Int("max-batch-files", 0, "max files per semantic request (0 = automatic)")
	maxBatchBytes := fs.Int("max-batch-bytes", 0, "max bytes per semantic request (0 = automatic)")
	supportsJSON := fs.String("supports-json-format", "", "send response_format=json_object (true|false; empty = provider default)")
	inputCost := fs.Float64("input-cost-per-mtok", 0, "input cost in USD per million tokens (0 = omit)")
	outputCost := fs.Float64("output-cost-per-mtok", 0, "output cost in USD per million tokens (0 = omit)")
	check := fs.Bool("check", false, "exit non-zero unless the corpus meets the release thresholds")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fixtures, err := eval.Load(*corpus)
	if err != nil {
		return err
	}
	var supportsJSONFormat *bool
	if strings.TrimSpace(*supportsJSON) != "" {
		v, err := strconv.ParseBool(strings.TrimSpace(*supportsJSON))
		if err != nil {
			return fmt.Errorf("--supports-json-format: %w", err)
		}
		supportsJSONFormat = &v
	}
	var prov provider.Provider
	switch *providerName {
	case "", "oracle":
		prov = eval.Oracle{}
	case "fake":
		prov = &provider.Fake{Responses: []string{`{"findings":[]}`}}
	default:
		key := os.Getenv("REVIEW_PROVIDER_API_KEY")
		if key == "" {
			return fmt.Errorf("no API key: set REVIEW_PROVIDER_API_KEY for provider %s", *providerName)
		}
		prov, err = provider.New(*providerName, provider.Config{
			BaseURL:            os.Getenv("REVIEW_PROVIDER_BASE_URL"),
			APIKey:             key,
			Model:              *model,
			MaxReasoningTokens: *maxReasoning,
			SupportsJSONFormat: supportsJSONFormat,
		})
		if err != nil {
			return err
		}
	}
	report, err := eval.RunWithOptions(context.Background(), fixtures, prov, eval.Options{
		Model:              *model,
		MaxReasoningTokens: *maxReasoning,
		MaxBatchFiles:      *maxBatchFiles,
		MaxBatchBytes:      *maxBatchBytes,
		SupportsJSONFormat: supportsJSONFormat,
		InputCostPerMTok:   *inputCost,
		OutputCostPerMTok:  *outputCost,
	})
	if err != nil {
		return err
	}
	md := report.Markdown()
	if *out != "" {
		if err := os.WriteFile(*out, []byte(md), 0o644); err != nil {
			return err
		}
	} else {
		fmt.Print(md)
	}
	if *check {
		c := report.Counts()
		if c.Recall() < 1.0 || c.P0P1Recall() < 1.0 || c.SeverityAccuracy() < 1.0 || c.Precision() < 1.0 ||
			c.FalsePositives > 0 || c.InvalidEvidence > 0 || c.CleanFindings > 0 {
			return fmt.Errorf("corpus does not meet release thresholds (precision=%.2f recall=%.2f p0p1=%.2f severity=%.2f fp=%d invalid=%d clean=%d)",
				c.Precision(), c.Recall(), c.P0P1Recall(), c.SeverityAccuracy(), c.FalsePositives, c.InvalidEvidence, c.CleanFindings)
		}
	}
	return nil
}

func splitRepo(s string) (string, string, error) {
	parts := strings.SplitN(strings.TrimSpace(s), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("--repository must be owner/name, got %q", s)
	}
	return parts[0], parts[1], nil
}

// hasLabel reports whether the issue carries the named label (case-insensitive).
func hasLabel(labels []struct {
	Name string `json:"name"`
}, want string) bool {
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l.Name), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}
