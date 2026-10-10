// Package config loads the engine's configuration from a YAML file, applies
// defaults, and folds in environment overrides used by the GitHub Action.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Mode selects run behavior.
type Mode string

const (
	ModeTracking  Mode = "tracking"
	ModeStateless Mode = "stateless"
)

// Config is the top-level engine configuration.
type Config struct {
	// Profile is the default profile when a run does not name one.
	Profile string `yaml:"profile"`
	// Mode selects tracking (persist + reconcile) or stateless (single run).
	Mode Mode `yaml:"mode"`
	// Provider selects and configures the language-model provider.
	Provider ProviderConfig `yaml:"provider"`
	// Review controls the analysis passes.
	Review ReviewConfig `yaml:"review"`
	// Archive controls publishing of findings to a bot branch.
	Archive ArchiveConfig `yaml:"archive"`
	// Store selects the tracking backend.
	Store StoreConfig `yaml:"store"`
	// IssueReview controls issue-close behavior.
	IssueReview IssueReviewConfig `yaml:"issueReview"`
	// Profiles holds per-profile overrides so one config serves many review
	// types (and, with a global config, many repositories).
	Profiles map[string]ProfileOverride `yaml:"profiles"`
	// Ignore is a list of glob patterns excluded from analysis.
	Ignore []string `yaml:"ignore"`
}

// StoreConfig selects the tracking backend. JSON is the default; SQLite is
// opt-in.
type StoreConfig struct {
	// SQLite enables the SQLite backend (default false).
	SQLite bool `yaml:"sqlite"`
	// Path is the SQLite database path. Empty uses <state-dir>/review.db.
	Path string `yaml:"path"`
}

// ProfileOverride adjusts review settings for a named profile.
type ProfileOverride struct {
	Rubric         []string `yaml:"rubric"`
	BlockThreshold string   `yaml:"blockThreshold"`
	Ignore         []string `yaml:"ignore"`
	MaxFiles       int      `yaml:"maxFiles"`
	MaxFileBytes   int      `yaml:"maxFileBytes"`
	Static         *bool    `yaml:"static"`
	Semantic       *bool    `yaml:"semantic"`
}

// ProviderConfig configures the model provider.
type ProviderConfig struct {
	Name    string `yaml:"name"`
	BaseURL string `yaml:"baseUrl"`
	Model   string `yaml:"model"`
	// APIKeyEnv names the environment variable holding the API key. When
	// empty, REVIEW_PROVIDER_API_KEY is used.
	APIKeyEnv string `yaml:"apiKeyEnv"`
}

// ReviewConfig controls analysis passes.
type ReviewConfig struct {
	// Static enables static analyzers.
	Static bool `yaml:"static"`
	// Semantic enables the model-backed semantic pass.
	Semantic bool `yaml:"semantic"`
	// Rubric is the list of review aspects for the semantic pass.
	Rubric []string `yaml:"rubric"`
	// MaxFiles caps how many changed files are sent for semantic review.
	MaxFiles int `yaml:"maxFiles"`
	// MaxFileBytes caps the size of each file sent for review.
	MaxFileBytes int `yaml:"maxFileBytes"`
	// BlockThreshold is the severity at or above which findings block.
	BlockThreshold string `yaml:"blockThreshold"`
}

// ArchiveConfig controls findings persistence and publishing.
type ArchiveConfig struct {
	Enabled bool   `yaml:"enabled"`
	Branch  string `yaml:"branch"`
	Dir     string `yaml:"dir"`
}

// IssueReviewConfig controls issue-close gating.
type IssueReviewConfig struct {
	// AutoClose closes an issue when all acceptance criteria are met.
	AutoClose   bool `yaml:"autoClose"`
	AutoReopen  bool `yaml:"autoReopen"`
	BlockMerge  bool `yaml:"blockMerge"`
	CommentOnly bool `yaml:"commentOnly"`
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		Profile: "pr",
		Mode:    ModeTracking,
		Provider: ProviderConfig{
			Name:      "openai-compatible",
			APIKeyEnv: "REVIEW_PROVIDER_API_KEY",
		},
		Review: ReviewConfig{
			Static:         true,
			Semantic:       true,
			Rubric:         DefaultRubric(),
			MaxFiles:       60,
			MaxFileBytes:   200_000,
			BlockThreshold: "P1",
		},
		Archive: ArchiveConfig{
			Enabled: true,
			Branch:  "review-artifacts",
			Dir:     "runs",
		},
		Store: StoreConfig{
			SQLite: false,
		},
		IssueReview: IssueReviewConfig{
			CommentOnly: true,
		},
	}
}

// DefaultRubric is the semantic review checklist.
func DefaultRubric() []string {
	return []string{
		"correctness",
		"concurrency and cancellation",
		"error handling",
		"security",
		"test quality",
		"missing implementation",
	}
}

// Load builds the effective configuration in layers: built-in defaults, then an
// optional global config (REVIEW_GLOBAL_CONFIG, else <user-config-dir>/revu/
// config.yaml), then the local file, then environment overrides. A missing file
// is not an error.
func Load(path string) (Config, error) {
	cfg := Default()
	if global := GlobalPath(); global != "" {
		if err := applyFile(&cfg, global); err != nil {
			return Config{}, err
		}
	}
	if path != "" {
		if err := applyFile(&cfg, path); err != nil {
			return Config{}, err
		}
	}
	applyEnv(&cfg)
	return cfg, nil
}

// GlobalPath returns the global config path, or "" when none is configured.
func GlobalPath() string {
	if v := os.Getenv("REVIEW_GLOBAL_CONFIG"); v != "" {
		return v
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return fmt.Sprintf("%s/revu/config.yaml", strings.TrimRight(dir, "/"))
	}
	return ""
}

func applyFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("config: parse %s: %w", path, err)
	}
	return nil
}

// ApplyProfile applies the named profile's overrides, if any.
func (c *Config) ApplyProfile(profile string) {
	o, ok := c.Profiles[profile]
	if !ok {
		return
	}
	if len(o.Rubric) > 0 {
		c.Review.Rubric = o.Rubric
	}
	if o.BlockThreshold != "" {
		c.Review.BlockThreshold = o.BlockThreshold
	}
	if o.Ignore != nil {
		c.Ignore = o.Ignore
	}
	if o.MaxFiles > 0 {
		c.Review.MaxFiles = o.MaxFiles
	}
	if o.MaxFileBytes > 0 {
		c.Review.MaxFileBytes = o.MaxFileBytes
	}
	if o.Static != nil {
		c.Review.Static = *o.Static
	}
	if o.Semantic != nil {
		c.Review.Semantic = *o.Semantic
	}
}

// Marshal renders the effective configuration as YAML.
func Marshal(cfg Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}

// SQLitePath returns the SQLite database path for the given state directory.
func (s StoreConfig) SQLitePath(stateDir string) string {
	if s.Path != "" {
		return s.Path
	}
	return fmt.Sprintf("%s/review.db", strings.TrimRight(stateDir, "/"))
}

// applyEnv folds GitHub Action inputs (exposed as REVIEW_* environment
// variables) into the configuration so a consumer needs only a single step.
func applyEnv(cfg *Config) {
	if v := os.Getenv("REVIEW_PROFILE"); v != "" {
		cfg.Profile = v
	}
	if v := os.Getenv("REVIEW_MODE"); v != "" {
		cfg.Mode = Mode(v)
	}
	if v := os.Getenv("REVIEW_PROVIDER"); v != "" {
		cfg.Provider.Name = v
	}
	if v := os.Getenv("REVIEW_PROVIDER_BASE_URL"); v != "" {
		cfg.Provider.BaseURL = v
	}
	if v := os.Getenv("REVIEW_MODEL"); v != "" {
		cfg.Provider.Model = v
	}
	if v := os.Getenv("REVIEW_ARCHIVE_BRANCH"); v != "" {
		cfg.Archive.Branch = v
	}
	if v := os.Getenv("REVIEW_BLOCK_THRESHOLD"); v != "" {
		cfg.Review.BlockThreshold = v
	}
	if v := os.Getenv("REVIEW_STATIC"); v != "" {
		cfg.Review.Static = parseBool(v, cfg.Review.Static)
	}
	if v := os.Getenv("REVIEW_SEMANTIC"); v != "" {
		cfg.Review.Semantic = parseBool(v, cfg.Review.Semantic)
	}
	if v := os.Getenv("REVIEW_ISSUE_AUTO_REOPEN"); v != "" {
		cfg.IssueReview.AutoReopen = parseBool(v, cfg.IssueReview.AutoReopen)
	}
	if v := os.Getenv("REVIEW_ISSUE_AUTO_CLOSE"); v != "" {
		cfg.IssueReview.AutoClose = parseBool(v, cfg.IssueReview.AutoClose)
	}
	if v := os.Getenv("REVIEW_ISSUE_BLOCK_MERGE"); v != "" {
		cfg.IssueReview.BlockMerge = parseBool(v, cfg.IssueReview.BlockMerge)
	}
	if v := os.Getenv("REVIEW_STORE_SQLITE"); v != "" {
		cfg.Store.SQLite = parseBool(v, cfg.Store.SQLite)
	}
	if v := os.Getenv("REVIEW_STORE_PATH"); v != "" {
		cfg.Store.Path = v
	}
}

func parseBool(s string, fallback bool) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return b
}

// APIKey resolves the provider API key from the configured environment
// variable (default REVIEW_PROVIDER_API_KEY).
func (p ProviderConfig) APIKey() string {
	env := p.APIKeyEnv
	if env == "" {
		env = "REVIEW_PROVIDER_API_KEY"
	}
	return os.Getenv(env)
}
