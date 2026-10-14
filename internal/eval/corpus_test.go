package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/greadee/review-engine/internal/provider"
)

// Release thresholds for the v1 PR-review claim. They are intentionally strict:
// the MVP is not declared complete unless the deterministic corpus meets them.
const (
	minP0P1Recall       = 1.0
	minSeverityAccuracy = 1.0
	maxFalsePositives   = 0
	maxInvalidEvidence  = 0
	maxCleanFindings    = 0
)

func corpusDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "corpus")
}

func TestCorpusDeterministic(t *testing.T) {
	fixtures, err := Load(corpusDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) < 4 {
		t.Fatalf("corpus too small: %d fixtures", len(fixtures))
	}
	report, err := Run(context.Background(), fixtures, Oracle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("\n%s", report.Markdown())

	c := report.Counts()
	if c.Recall() < 1.0 {
		t.Errorf("recall = %.2f, want 1.0 (%d/%d detected)", c.Recall(), c.Detected, c.Expected)
	}
	if c.P0P1Recall() < minP0P1Recall {
		t.Errorf("P0/P1 recall = %.2f, want >= %.2f", c.P0P1Recall(), minP0P1Recall)
	}
	if c.SeverityAccuracy() < minSeverityAccuracy {
		t.Errorf("severity accuracy = %.2f, want >= %.2f", c.SeverityAccuracy(), minSeverityAccuracy)
	}
	if c.FalsePositives > maxFalsePositives {
		t.Errorf("false positives = %d, want <= %d", c.FalsePositives, maxFalsePositives)
	}
	if c.InvalidEvidence > maxInvalidEvidence {
		t.Errorf("findings with invalid evidence = %d, want <= %d", c.InvalidEvidence, maxInvalidEvidence)
	}
	if c.CleanFindings > maxCleanFindings {
		t.Errorf("findings on clean/adversarial fixtures = %d, want <= %d", c.CleanFindings, maxCleanFindings)
	}
	if c.CleanCovered != c.CleanFixtures {
		t.Errorf("clean fixtures with full coverage = %d/%d", c.CleanCovered, c.CleanFixtures)
	}
	for _, f := range report.Fixtures {
		if f.Clean && f.Verdict != "Approve" {
			t.Errorf("clean fixture %s verdict = %q, want Approve", f.Name, f.Verdict)
		}
		if f.Detected != f.Expected {
			t.Errorf("fixture %s detected %d/%d seeded defects", f.Name, f.Detected, f.Expected)
		}
	}
}

// TestCorpusPinnedModel runs the optional evaluation against a real, pinned
// model. It is skipped unless REVIEW_EVAL_MODEL and REVIEW_PROVIDER_API_KEY are
// set, so CI stays hermetic. Metrics are logged for publishing with the
// milestone; only hermetic invariants are asserted.
func TestCorpusPinnedModel(t *testing.T) {
	model := os.Getenv("REVIEW_EVAL_MODEL")
	key := os.Getenv("REVIEW_PROVIDER_API_KEY")
	if model == "" || key == "" {
		t.Skip("set REVIEW_EVAL_MODEL and REVIEW_PROVIDER_API_KEY to run the pinned-model evaluation")
	}
	base := os.Getenv("REVIEW_PROVIDER_BASE_URL")
	if base == "" {
		t.Setenv(provider.AllowedBaseURLsEnv, provider.DefaultBaseURL)
	}
	prov, err := provider.New("openai-compatible", provider.Config{BaseURL: base, APIKey: key, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	fixtures, err := Load(corpusDir(t))
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), fixtures, prov)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("\n%s", report.Markdown())
	for _, f := range report.Fixtures {
		if f.Clean && !f.CoverageComplete {
			t.Errorf("clean fixture %s had incomplete coverage", f.Name)
		}
	}
}
