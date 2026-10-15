// Package analyzers runs static tooling over a repository and converts
// diagnostics into findings. Analyzers are pluggable per ecosystem; only Go is
// implemented today.
package analyzers

import (
	"context"
	"os"
	"strings"

	"github.com/greadee/review-engine/internal/findings"
)

// Target is the input to a static analyzer.
type Target struct {
	// Dir is the repository working directory.
	Dir string
	// Files are the reviewed paths relative to Dir (changed for a PR, all for
	// an audit).
	Files []string
	// AllFiles are all tracked paths relative to Dir, used for repo-wide
	// reference analysis.
	AllFiles []string
	// WholeRepo is true for audit/sprint profiles.
	WholeRepo bool
}

// Analyzer inspects a target and returns findings.
type Analyzer interface {
	Name() string
	Analyze(ctx context.Context, t Target) ([]findings.Finding, error)
}

// Default returns the analyzers enabled by default. Each analyzer is a no-op
// when its ecosystem is not detected or its tooling is unavailable.
func Default() []Analyzer {
	return []Analyzer{NewGo(), NewPython(), NewTypeScript()}
}

// childEnv returns a minimal environment for analyzer subprocesses. Repository
// code is untrusted, so a child process must not inherit the provider key, the
// GitHub token, or any other secret from the parent. Only variables needed for
// a build to run are passed through.
func childEnv() []string {
	keep := map[string]bool{
		"PATH": true, "HOME": true, "TMPDIR": true, "TMP": true, "TEMP": true,
		"LANG": true, "LC_ALL": true, "TZ": true,
		"GOCACHE": true, "GOPATH": true, "GOMODCACHE": true, "GOROOT": true,
		"GOFLAGS": true, "GO111MODULE": true,
		"SYSTEMROOT": true, "PATHEXT": true,
	}
	var out []string
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 && keep[strings.ToUpper(kv[:i])] {
			out = append(out, kv)
		}
	}
	return out
}
