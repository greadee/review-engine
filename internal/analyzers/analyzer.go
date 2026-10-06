// Package analyzers runs static tooling over a repository and converts
// diagnostics into findings. Analyzers are pluggable per ecosystem; only Go is
// implemented today.
package analyzers

import (
	"context"

	"github.com/greadee/review-engine/internal/findings"
)

// Target is the input to a static analyzer.
type Target struct {
	// Dir is the repository working directory.
	Dir string
	// Files are the reviewed paths relative to Dir.
	Files []string
}

// Analyzer inspects a target and returns findings.
type Analyzer interface {
	Name() string
	Analyze(ctx context.Context, t Target) ([]findings.Finding, error)
}

// Default returns the analyzers enabled by default. The Go analyzer is a no-op
// when no Go module is present.
func Default() []Analyzer {
	return []Analyzer{NewGo()}
}
