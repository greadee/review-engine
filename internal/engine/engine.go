// Package engine orchestrates the review pipeline: scope resolution, static
// analysis, semantic review, normalization, lifecycle tracking, and rendering.
//
// A review runs in one of two shapes:
//   - RunRange: collect + finalize in one process (local use, same-repo CI).
//   - Collect then Finalize: split across a no-secret and a secret-bearing job
//     for fork-safe CI.
package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/greadee/review-engine/internal/analyzers"
	"github.com/greadee/review-engine/internal/config"
	"github.com/greadee/review-engine/internal/detectors"
	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/provider"
	"github.com/greadee/review-engine/internal/report"
	"github.com/greadee/review-engine/internal/reviewers"
	"github.com/greadee/review-engine/internal/store"
	"github.com/greadee/review-engine/internal/vcs"
)

// Options configures the engine.
type Options struct {
	Config    config.Config
	Repo      *vcs.Repo
	Provider  provider.Provider
	Analyzers []analyzers.Analyzer
	Reviewer  *reviewers.Semantic
	Store     store.Store
	Now       func() time.Time
	// PR is the pull request number, recorded in the bundle so the finalize
	// phase can bind the artifact to the trusted event.
	PR int
}

// Engine runs profiles over a repository.
type Engine struct {
	opts Options
}

// New builds an engine, filling in defaults.
func New(opts Options) *Engine {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Analyzers == nil {
		opts.Analyzers = append(analyzers.Default(), detectors.Default()...)
	}
	if opts.Store == nil {
		opts.Store = store.Stateless{}
	}
	return &Engine{opts: opts}
}

// Result is the outcome of one run.
type Result struct {
	Run      report.Run
	Findings []findings.Finding
	// Persisted reports whether the persist policy allowed the run to be
	// written to a store. Callers must not write local or remote findings when
	// it is false.
	Persisted bool
}

// Persist applies the persist policy: whether the findings should be written to
// any store. Mode "always" persists; "on-findings" only when at least one
// finding exists; "on-risk" only when a finding is at or above the risk
// threshold; "never" never persists.
func Persist(cfg config.Config, fs []findings.Finding) bool {
	switch strings.ToLower(strings.TrimSpace(cfg.Persist.Mode)) {
	case "never":
		return false
	case "on-findings":
		return len(fs) > 0
	case "on-risk":
		risk := findings.Severity(strings.ToUpper(strings.TrimSpace(cfg.Persist.Risk)))
		if !risk.Valid() {
			risk = findings.P2
		}
		for _, f := range fs {
			if f.Severity.Rank() >= 0 && f.Severity.Rank() <= risk.Rank() {
				return true
			}
		}
		return false
	default: // always
		return true
	}
}

// RunRange collects and finalizes in one process.
func (e *Engine) RunRange(ctx context.Context, profile, base, head, repository, ref string) (Result, error) {
	b, err := e.Collect(ctx, profile, base, head, repository, ref)
	if err != nil {
		return Result{}, err
	}
	return e.Finalize(ctx, b)
}

// gatherStats reports how file contents were gathered for semantic review.
type gatherStats struct {
	files          []reviewers.FileContent
	skipped        int
	truncated      int
	truncatedBytes int
}

func (e *Engine) gather(ref string, paths []string, wholeRepo bool) gatherStats {
	maxFiles := e.opts.Config.Review.MaxFiles
	maxBytes := e.opts.Config.Review.MaxFileBytes
	if maxFiles <= 0 {
		maxFiles = 60
	}
	// Whole-repo profiles (audit/sprint) review the entire codebase; the maxFiles
	// cap exists for PR diffs where it makes sense to bound token usage.  It
	// must not truncate audit or sprint scans.
	if wholeRepo {
		maxFiles = 0 // disable the cap: 0 means "unlimited"
	}
	var g gatherStats
	for _, p := range paths {
		if maxFiles > 0 && len(g.files) >= maxFiles {
			g.skipped++
			continue
		}
		data, err := e.opts.Repo.File(ref, p)
		if err != nil {
			g.skipped++
			continue
		}
		if maxBytes > 0 && len(data) > maxBytes {
			g.truncated++
			g.truncatedBytes += len(data) - maxBytes
			data = data[:maxBytes]
		}
		g.files = append(g.files, reviewers.FileContent{Path: p, Content: string(data)})
	}
	return g
}

func threshold(s string) findings.Severity {
	sev := findings.Severity(strings.ToUpper(strings.TrimSpace(s)))
	if sev.Valid() {
		return sev
	}
	return findings.P1
}

func runID(profile, head string, now time.Time) string {
	short := head
	if len(short) > 12 {
		short = short[:12]
	}
	if short == "" {
		return fmt.Sprintf("%s@%s", profile, now.UTC().Format("20060102T150405Z"))
	}
	return fmt.Sprintf("%s@%s", profile, short)
}
