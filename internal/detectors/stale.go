package detectors

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/greadee/review-engine/internal/analyzers"
	"github.com/greadee/review-engine/internal/findings"
)

// StaleReference flags documentation that points at repository paths which no
// longer exist — typically left behind after a removal or rename.
type StaleReference struct{}

// NewStaleReference returns a stale-reference detector.
func NewStaleReference() *StaleReference { return &StaleReference{} }

// Name implements analyzers.Analyzer.
func (*StaleReference) Name() string { return "stale-reference" }

var (
	backtickSpan = regexp.MustCompile("`([^`\n]+)`")
	markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	repoPath     = regexp.MustCompile(`^\.?[\w][\w./-]*\.(go|md|json|ya?ml|ts|tsx|js|py|toml|schema|sql|css|html|sh)$`)
)

type candidate struct {
	value string
	link  bool
}

// Analyze implements analyzers.Analyzer. It scans all Markdown files, since a
// removed path is often referenced by a document that is not part of the diff.
func (StaleReference) Analyze(_ context.Context, t analyzers.Target) ([]findings.Finding, error) {
	var out []findings.Finding
	seen := map[string]bool{}
	for _, docPath := range allFiles(t) {
		if isSkippedPath(docPath) || !hasSuffix(docPath, ".md") {
			continue
		}
		data, err := readFile(t.Dir, docPath)
		if err != nil {
			continue
		}
		var candidates []candidate
		for _, m := range backtickSpan.FindAllStringSubmatch(string(data), -1) {
			candidates = append(candidates, candidate{value: m[1]})
		}
		for _, m := range markdownLink.FindAllStringSubmatch(string(data), -1) {
			candidates = append(candidates, candidate{value: m[1], link: true})
		}
		docDir := path.Dir(filepathSlash(docPath))
		for _, c := range candidates {
			clean := cleanRepoPath(c.value)
			if clean == "" || !repoPath.MatchString(clean) {
				continue
			}
			// A backticked token only counts when it is path-like; markdown
			// links are always checked relative to the document.
			if !c.link && !strings.Contains(c.value, "/") {
				continue
			}
			if existsWithin(t.Dir, clean) || (docDir != "." && existsWithin(t.Dir, path.Join(docDir, clean))) {
				continue
			}
			key := docPath + "\x00" + clean
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, findings.Finding{
				Title:          "Documentation references a path that does not exist",
				Classification: findings.Documentation,
				Severity:       findings.P3,
				Evidence:       findings.Evidence{File: filepathSlash(docPath), Snippet: clean},
				Impact:         "Stale reference misleads readers and tools.",
				Recommendation: "Update or remove the reference.",
				Detector:       "detector.stale-reference",
				Anchor:         "stale:" + clean,
			})
			if len(out) >= maxFindings {
				return out, nil
			}
		}
	}
	return out, nil
}

func existsWithin(dir, rel string) bool {
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil
}

func cleanRepoPath(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "#?"); i >= 0 {
		s = s[:i]
	}
	// Strip only a leading "./" or "/"; a lone leading dot (e.g. ".github/")
	// is part of the path.
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimPrefix(s, "/")
	if strings.Contains(s, "://") || strings.HasPrefix(s, "mailto:") || strings.ContainsAny(s, " <>|") {
		return ""
	}
	return s
}
