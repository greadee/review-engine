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
// It needs no repository working tree.
func (e *Engine) RunIssue(ctx context.Context, issue vcs.Issue) (Result, error) {
	set := findings.NewSet()
	for _, f := range issues.Findings(issue) {
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
