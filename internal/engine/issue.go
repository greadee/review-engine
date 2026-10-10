package engine

import (
	"context"
	"fmt"

	"github.com/greadee/review-engine/internal/config"
	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/issues"
	"github.com/greadee/review-engine/internal/profiles"
	"github.com/greadee/review-engine/internal/report"
	"github.com/greadee/review-engine/internal/store"
	"github.com/greadee/review-engine/internal/vcs"
)

// RunIssue reviews an issue's delivered work against its acceptance criteria.
// It needs no repository working tree, but when one is available it reads
// acceptance criteria from the docs the issue references.
func (e *Engine) RunIssue(ctx context.Context, issue vcs.Issue) (Result, error) {
	set := findings.NewSet()
	for _, f := range issues.FindingsWith(issue, e.issueDocReader()) {
		set.Add(f)
	}
	runID := fmt.Sprintf("issue-%d@%s", issue.Number, issue.State)

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
			Profile:        profiles.Issue,
			Repository:     fmt.Sprintf("issue#%d", issue.Number),
			Generated:      e.opts.Now().UTC(),
			Findings:       cur,
			Resolved:       resolved,
			BlockThreshold: threshold(e.opts.Config.Review.BlockThreshold),
		},
		Findings: cur,
	}, nil
}

// issueDocReader reads referenced Markdown docs from the working tree, when a
// repository is available, so acceptance criteria kept in docs are honored.
func (e *Engine) issueDocReader() issues.DocReader {
	if e.opts.Repo == nil {
		return nil
	}
	repo := e.opts.Repo
	return func(p string) (string, bool) {
		data, err := repo.File("", p)
		if err != nil {
			return "", false
		}
		return string(data), true
	}
}
