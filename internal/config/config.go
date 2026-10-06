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
	// IssueReview controls issue-close behavior.
	IssueReview IssueReviewConfig `yaml:"issueReview"`
	// Ignore is a list of glob patterns excluded from analysis.
	Ignore []string `yaml:"ignore"`
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

// Load reads path (when non-empty) over the defaults, then applies
// environment overrides. A missing file is not an error.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("config: read %s: %w", path, err)
		}
		if err == nil {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
			}
		}
	}
	applyEnv(&cfg)
	return cfg, nil
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
	if v := os.Getenv("REVIEW_ISSUE_BLOCK_MERGE"); v != "" {
		cfg.IssueReview.BlockMerge = parseBool(v, cfg.IssueReview.BlockMerge)
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
