package engine

import (
	"context"
	"fmt"

	"github.com/greadee/review-engine/internal/analyzers"
	"github.com/greadee/review-engine/internal/bundle"
	"github.com/greadee/review-engine/internal/config"
	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/profiles"
	"github.com/greadee/review-engine/internal/report"
	"github.com/greadee/review-engine/internal/reviewers"
	"github.com/greadee/review-engine/internal/scope"
	"github.com/greadee/review-engine/internal/store"
)

// Collect runs the untrusted, secret-free phase: it resolves scope, gathers
// file contents, and runs the static analyzers and detectors. It must not
// perform any network write or model call, so it is safe to run against an
// untrusted pull request.
func (e *Engine) Collect(ctx context.Context, profile, base, head, repository, ref string) (bundle.Bundle, error) {
	if e.opts.Repo == nil {
		return bundle.Bundle{}, fmt.Errorf("engine: repository is required to collect")
	}
	changes, err := e.opts.Repo.Changes(base, head)
	if err != nil {
		return bundle.Bundle{}, fmt.Errorf("engine: diff %s..%s: %w", base, head, err)
	}
	sc := scope.Resolve(profile, changes, e.opts.Config.Ignore)
	allFiles, _ := e.opts.Repo.ListFiles(head)
	wholeRepo := profile == profiles.Audit || profile == profiles.Sprint
	if wholeRepo {
		sc.Files = allFiles
	}

	set := findings.NewSet()
	if e.opts.Config.Review.Static {
		target := analyzers.Target{
			Dir:       e.opts.Repo.Dir,
			Files:     sc.Files,
			AllFiles:  allFiles,
			WholeRepo: wholeRepo,
		}
		for _, a := range e.opts.Analyzers {
			found, err := a.Analyze(ctx, target)
			if err != nil {
				return bundle.Bundle{}, fmt.Errorf("engine: analyzer %s: %w", a.Name(), err)
			}
			for _, f := range found {
				set.Add(f)
			}
		}
	}

	return bundle.Bundle{
		Version:        bundle.Version,
		Profile:        profile,
		Repository:     repository,
		Ref:            ref,
		Base:           base,
		Head:           head,
		RunID:          runID(profile, head, e.opts.Now()),
		Generated:      e.opts.Now().UTC(),
		BlockThreshold: e.opts.Config.Review.BlockThreshold,
		Rubric:         e.opts.Config.Review.Rubric,
		Files:          e.gather(head, sc.Files),
		StaticFindings: set.Items(),
	}, nil
}

// Finalize runs the secret-bearing phase: it applies the model-backed semantic
// review to the collected files, merges and normalizes findings, updates
// tracking, and builds the report. It does not need the repository working
// tree, so it can run in a job that never checks out untrusted code.
func (e *Engine) Finalize(ctx context.Context, b bundle.Bundle) (Result, error) {
	set := findings.NewSet()
	for _, f := range b.StaticFindings {
		set.Add(f)
	}

	if e.opts.Config.Review.Semantic {
		if e.opts.Reviewer == nil || e.opts.Provider == nil {
			return Result{}, fmt.Errorf("engine: semantic review is enabled but no provider is configured")
		}
		rubric := b.Rubric
		if len(rubric) == 0 {
			rubric = e.opts.Config.Review.Rubric
		}
		found, err := e.opts.Reviewer.Review(ctx, reviewers.Input{
			Profile: b.Profile,
			Rubric:  rubric,
			Files:   b.Files,
		})
		if err != nil {
			return Result{}, fmt.Errorf("engine: semantic review: %w", err)
		}
		for _, f := range found {
			set.Add(f)
		}
	}

	prev, err := e.opts.Store.Load(ctx)
	if err != nil {
		return Result{}, err
	}
	cur := findings.Lifecycle(prev.Findings, set.Items(), b.RunID)
	resolved := findings.Compare(prev.Findings, cur)
	if e.opts.Config.Mode != config.ModeStateless {
		if err := e.opts.Store.Save(ctx, store.State{RunID: b.RunID, Findings: findings.MergeTracking(cur, resolved)}); err != nil {
			return Result{}, err
		}
	}

	return Result{
		Run: report.Run{
			ID:             b.RunID,
			Profile:        b.Profile,
			Repository:     b.Repository,
			Ref:            b.Ref,
			Base:           b.Base,
			Head:           b.Head,
			Generated:      e.opts.Now().UTC(),
			Findings:       cur,
			Resolved:       resolved,
			BlockThreshold: threshold(b.BlockThreshold),
		},
		Findings: cur,
	}, nil
}
