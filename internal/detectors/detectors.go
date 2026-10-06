// Package detectors implements the engine's specialized, non-generic analyses:
// orphaned/test-only wiring, stale references, fail-open guards, and
// unfinished markers. They supplement the static analyzers and are what makes
// the engine more than a linter.
package detectors

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/greadee/review-engine/internal/analyzers"
)

// Default returns the built-in detectors.
func Default() []analyzers.Analyzer {
	return []analyzers.Analyzer{
		NewOrphanGo(),
		NewStaleReference(),
		NewFailOpen(),
		NewUnfinished(),
	}
}

// maxFindings caps a single detector's output to keep reports readable.
const maxFindings = 50

func allFiles(t analyzers.Target) []string {
	if len(t.AllFiles) > 0 {
		return t.AllFiles
	}
	return t.Files
}

// underReview reports whether path should be reported on: all files for a
// whole-repo audit, otherwise only the explicitly reviewed files.
func underReview(t analyzers.Target, path string) bool {
	if t.WholeRepo || len(t.Files) == 0 {
		return true
	}
	for _, f := range t.Files {
		if f == path {
			return true
		}
	}
	return false
}

func hasSuffix(path string, suffixes ...string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(path, s) {
			return true
		}
	}
	return false
}

func isSkippedPath(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, p := range parts {
		switch p {
		case "vendor", "node_modules", "testdata", ".git":
			return true
		}
	}
	return false
}

func readFile(dir, path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
}

func fileLines(data []byte) []string {
	return strings.Split(string(data), "\n")
}

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
