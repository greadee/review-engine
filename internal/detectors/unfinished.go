package detectors

import (
	"context"
	"regexp"
	"strings"

	"github.com/greadee/review-engine/internal/analyzers"
	"github.com/greadee/review-engine/internal/findings"
)

// Unfinished flags markers that indicate incomplete production code:
// completion-marker comments and not-implemented stubs.
type Unfinished struct{}

// NewUnfinished returns an unfinished-work detector.
func NewUnfinished() *Unfinished { return &Unfinished{} }

// Name implements analyzers.Analyzer.
func (*Unfinished) Name() string { return "unfinished" }

var (
	markerRe = regexp.MustCompile(`(?i)\b(TODO|FIXME|XXX)\b`)
	notImpl  = regexp.MustCompile(`(?i)not[ _]?implemented|unimplemented`)
)

// Analyze implements analyzers.Analyzer.
func (Unfinished) Analyze(_ context.Context, t analyzers.Target) ([]findings.Finding, error) {
	files := t.Files
	if t.WholeRepo || len(files) == 0 {
		files = allFiles(t)
	}
	var out []findings.Finding
	for _, path := range files {
		if isSkippedPath(path) || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := readFile(t.Dir, path)
		if err != nil {
			continue
		}
		for i, line := range fileLines(data) {
			// Skip regex definitions and other lines that merely name the
			// markers (including this detector's own source).
			if strings.Contains(line, "regexp.") {
				continue
			}
			isMarker := markerRe.MatchString(line)
			trimmed := strings.TrimSpace(line)
			isNotImpl := notImpl.MatchString(line) &&
				(strings.Contains(line, "panic(") || strings.HasPrefix(trimmed, "//"))
			if !isMarker && !isNotImpl {
				continue
			}
			snippet := trimmed
			sev := findings.P3
			title := "Incomplete-work marker"
			if isNotImpl {
				sev = findings.P2
				title = "Not-implemented code path"
			}
			out = append(out, findings.Finding{
				Title:          title,
				Classification: findings.TechnicalDebt,
				Severity:       sev,
				Evidence:       findings.Evidence{File: filepathSlash(path), Line: i + 1, Snippet: snippet},
				Impact:         "Signals behavior that may be missing or stubbed.",
				Recommendation: "Implement, or record as a tracked issue if intentional.",
				Detector:       "detector.unfinished",
				Anchor:         "unfinished:" + snippet,
			})
			if len(out) >= maxFindings {
				return out, nil
			}
		}
	}
	return out, nil
}
