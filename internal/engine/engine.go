// Package engine orchestrates the review pipeline: scope resolution, static
// analysis, semantic review, normalization, lifecycle tracking, and rendering.
package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/greadee/review-engine/internal/analyzers"
	"github.com/greadee/review-engine/internal/config"
	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/provider"
	"github.com/greadee/review-engine/internal/report"
	"github.com/greadee/review-engine/internal/reviewers"
	"github.com/greadee/review-engine/internal/scope"
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
		opts.Analyzers = analyzers.Default()
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

// RunRange reviews repository changes between base and head using profile.
func (e *Engine) RunRange(ctx context.Context, profile, base, head, repository, ref string) (Result, error) {
	changes, err := e.opts.Repo.Changes(base, head)
	if err != nil {
		return Result{}, fmt.Errorf("engine: diff %s..%s: %w", base, head, err)
	}
	sc := scope.Resolve(profile, changes, e.opts.Config.Ignore)

	set := findings.NewSet()
	if e.opts.Config.Review.Static {
		target := analyzers.Target{Dir: e.opts.Repo.Dir, Files: sc.Files}
		for _, a := range e.opts.Analyzers {
			found, err := a.Analyze(ctx, target)
			if err != nil {
				return Result{}, fmt.Errorf("engine: analyzer %s: %w", a.Name(), err)
			}
			for _, f := range found {
				set.Add(f)
			}
		}
	}

	if e.opts.Config.Review.Semantic && e.opts.Reviewer != nil && e.opts.Provider != nil {
		files := e.gather(head, sc.Files)
		found, err := e.opts.Reviewer.Review(ctx, reviewers.Input{
			Profile: profile,
			Rubric:  e.opts.Config.Review.Rubric,
			Files:   files,
		})
		if err != nil {
			return Result{}, fmt.Errorf("engine: semantic review: %w", err)
		}
		for _, f := range found {
			set.Add(f)
		}
	}

	runID := runID(profile, head, e.opts.Now())
	prev, err := e.opts.Store.Load(ctx)
	if err != nil {
		return Result{}, err
	}
	cur := findings.Lifecycle(prev.Findings, set.Items(), runID)
	resolved := findings.Compare(prev.Findings, cur)
	if e.opts.Config.Mode != config.ModeStateless {
		if err := e.opts.Store.Save(ctx, store.State{RunID: runID, Findings: findings.MergeTracking(cur, resolved)}); err != nil {
			return Result{}, err
		}
	}

	return Result{
		Run: report.Run{
			ID:             runID,
			Profile:        profile,
			Repository:     repository,
			Ref:            ref,
			Base:           base,
			Head:           head,
			Generated:      e.opts.Now().UTC(),
			Findings:       cur,
			Resolved:       resolved,
			BlockThreshold: threshold(e.opts.Config.Review.BlockThreshold),
		},
		Findings: cur,
	}, nil
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
