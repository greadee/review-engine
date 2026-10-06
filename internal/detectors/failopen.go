package detectors

import (
	"context"
	"strings"

	"github.com/greadee/review-engine/internal/analyzers"
	"github.com/greadee/review-engine/internal/findings"
)

// FailOpen flags a common Go pattern where a nil check short-circuits a guard:
//
//	if s.policy != nil && !s.policy.Allowed(...) { return err }
//
// When the dependency is nil the guard is skipped, so a missing configuration
// silently disables the check. The detector surfaces the pattern for review; it
// is intentionally a prompt, not a proof.
type FailOpen struct{}

// NewFailOpen returns a fail-open detector.
func NewFailOpen() *FailOpen { return &FailOpen{} }

// Name implements analyzers.Analyzer.
func (*FailOpen) Name() string { return "fail-open" }

// Analyze implements analyzers.Analyzer.
func (FailOpen) Analyze(_ context.Context, t analyzers.Target) ([]findings.Finding, error) {
	files := t.Files
	if t.WholeRepo || len(files) == 0 {
		files = allFiles(t)
	}
	var out []findings.Finding
	seen := map[string]bool{}
	for _, path := range files {
		if isSkippedPath(path) || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := readFile(t.Dir, path)
		if err != nil {
			continue
		}
		for i, line := range fileLines(data) {
			idx := strings.Index(line, "!= nil &&")
			if idx < 0 || !strings.HasPrefix(strings.TrimSpace(line), "if ") {
				continue
			}
			// Only dependency-style checks (a field/selector on the left) are
			// interesting; a bare "err != nil" is ordinary error handling.
			if !strings.Contains(line[:idx], ".") {
				continue
			}
			snippet := strings.TrimSpace(line)
			key := filepathSlash(path) + "\x00" + snippet
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, findings.Finding{
				Title:          "Possible fail-open guard: nil check short-circuits a check",
				Classification: findings.Reliability,
				Severity:       findings.P2,
				Evidence:       findings.Evidence{File: filepathSlash(path), Line: i + 1, Snippet: snippet},
				Impact:         "A missing dependency can silently disable the guarded check.",
				Recommendation: "Fail closed when the dependency is absent, or document why open is safe.",
				Detector:       "detector.fail-open",
				Anchor:         "fail-open:" + snippet,
			})
			if len(out) >= maxFindings {
				return out, nil
			}
		}
	}
	return out, nil
}
