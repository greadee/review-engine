package engine

import (
	"context"
	"fmt"
	"strings"

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
	// Record the resolved head SHA so the finalize phase can bind the artifact
	// to the trusted workflow event.
	headSHA, err := e.opts.Repo.HeadSHA(head)
	if err != nil {
		headSHA = head
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

	gathered := e.gather(head, sc.Files, wholeRepo)
	return bundle.Bundle{
		Version:        bundle.Version,
		Profile:        profile,
		Repository:     repository,
		PR:             e.opts.PR,
		Ref:            ref,
		Base:           base,
		Head:           headSHA,
		RunID:          runID(profile, headSHA, e.opts.Now()),
		Generated:      e.opts.Now().UTC(),
		BlockThreshold: e.opts.Config.Review.BlockThreshold,
		Rubric:         e.opts.Config.Review.Rubric,
		Selected:       len(sc.Files),
		Skipped:        gathered.skipped,
		Truncated:      gathered.truncated,
		TruncatedBytes: gathered.truncatedBytes,
		Files:          gathered.files,
		StaticFindings: set.Items(),
	}, nil
}

// Finalize runs the secret-bearing phase: it applies the model-backed semantic
// review to the collected files, merges and normalizes findings, updates
// tracking, and builds the report. It does not need the repository working
// tree, so it can run in a job that never checks out untrusted code.
//
// The bundle is treated as untrusted input: it is schema-validated, every
// finding must be well formed, and each finding's evidence must correspond to
// the bundled source or the finding is marked unverified and cannot block. The
// block threshold comes from trusted configuration, never from the bundle.
func (e *Engine) Finalize(ctx context.Context, b bundle.Bundle) (Result, error) {
	if err := b.Validate(); err != nil {
		return Result{}, err
	}
	source := sourceIndex(b.Files)
	set := findings.NewSet()
	for _, f := range b.StaticFindings {
		if err := f.Validate(); err != nil {
			continue
		}
		set.Add(f)
	}

	coverage := report.Coverage{
		Selected:       b.Selected,
		Skipped:        b.Skipped,
		Truncated:      b.Truncated,
		TruncatedBytes: b.TruncatedBytes,
		Threshold:      e.opts.Config.Review.CoverageThreshold,
	}
	if coverage.Selected == 0 {
		coverage.Selected = len(b.Files)
	}

	if e.opts.Config.Review.Semantic {
		if e.opts.Reviewer == nil || e.opts.Provider == nil {
			return Result{}, fmt.Errorf("engine: semantic review is enabled but no provider is configured")
		}
		rubric := e.opts.Config.Review.Rubric
		if len(rubric) == 0 {
			rubric = b.Rubric
		}
		found, semCov, err := e.opts.Reviewer.Review(ctx, reviewers.Input{
			Profile: b.Profile,
			Rubric:  rubric,
			Files:   b.Files,
		})
		if err != nil {
			return Result{}, fmt.Errorf("engine: semantic review: %w", err)
		}
		coverage.Analyzed = semCov.Analyzed
		coverage.FailedBatches = semCov.Failed
		for _, f := range found {
			if err := f.Validate(); err != nil {
				continue
			}
			set.Add(f)
		}
	} else {
		// No semantic pass: the whole selected scope was covered by static analysis.
		coverage.Analyzed = coverage.Selected
	}

	cur := set.Items()
	for i := range cur {
		if !evidenceMatches(cur[i], source) {
			cur[i].Unverified = true
		}
	}

	prev, err := e.opts.Store.Load(ctx)
	if err != nil {
		return Result{}, err
	}
	tracked := findings.Lifecycle(prev.Findings, cur, b.RunID)
	resolved := findings.Compare(prev.Findings, tracked)
	persisted := Persist(e.opts.Config, tracked)
	if persisted && e.opts.Config.Mode != config.ModeStateless {
		if err := e.opts.Store.Save(ctx, store.State{RunID: b.RunID, Findings: findings.MergeTracking(tracked, resolved)}); err != nil {
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
			Findings:       tracked,
			Resolved:       resolved,
			BlockThreshold: threshold(e.opts.Config.Review.BlockThreshold),
			Coverage:       coverage,
		},
		Findings:  tracked,
		Persisted: persisted,
	}, nil
}

// sourceIndex maps normalized paths to the bundled file contents.
func sourceIndex(files []reviewers.FileContent) map[string]string {
	idx := make(map[string]string, len(files))
	for _, f := range files {
		idx[evidenceKey(f.Path)] = f.Content
	}
	return idx
}

func evidenceKey(path string) string {
	p := strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	return strings.ToLower(p)
}

// evidenceMatches reports whether a finding cites a file present in the
// reviewed source with an in-range line. It deliberately does not require the
// snippet to appear in the file: static analyzers put the diagnostic message
// there rather than source text, and the security requirement is that a
// blocking finding cites a verified file and line. Findings that fail this are
// marked unverified and cannot block.
func evidenceMatches(f findings.Finding, source map[string]string) bool {
	key := evidenceKey(f.Evidence.File)
	if key == "" {
		return false
	}
	content, ok := source[key]
	if !ok {
		return false
	}
	if f.Evidence.Line > 0 && f.Evidence.Line > strings.Count(content, "\n")+1 {
		return false
	}
	return true
}
