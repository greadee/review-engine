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
}

// RunRange collects and finalizes in one process.
func (e *Engine) RunRange(ctx context.Context, profile, base, head, repository, ref string) (Result, error) {
	b, err := e.Collect(ctx, profile, base, head, repository, ref)
	if err != nil {
		return Result{}, err
	}
	return e.Finalize(ctx, b)
}

func (e *Engine) gather(ref string, paths []string) []reviewers.FileContent {
	maxFiles := e.opts.Config.Review.MaxFiles
	maxBytes := e.opts.Config.Review.MaxFileBytes
	if maxFiles <= 0 {
		maxFiles = 60
	}
	var out []reviewers.FileContent
	for _, p := range paths {
		if len(out) >= maxFiles {
			break
		}
		data, err := e.opts.Repo.File(ref, p)
		if err != nil {
			continue
		}
		if maxBytes > 0 && len(data) > maxBytes {
			data = data[:maxBytes]
		}
		out = append(out, reviewers.FileContent{Path: p, Content: string(data)})
	}
	return out
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
