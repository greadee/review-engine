package engine

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/greadee/review-engine/internal/report"
	"github.com/greadee/review-engine/internal/vcs"
)

// Publish archives the run's findings (JSON + Markdown) to the configured bot
// branch and returns the URL of the JSON source file. It never writes to the
// default or PR branch.
func (e *Engine) Publish(ctx context.Context, gh *vcs.GitHub, owner, repo string, res Result) (string, error) {
	if !e.opts.Config.Archive.Enabled {
		return "", nil
	}
	branch := e.opts.Config.Archive.Branch
	if branch == "" {
		branch = "review-artifacts"
	}
	dir := e.opts.Config.Archive.Dir
	if dir == "" {
		dir = "runs"
	}
	base := path.Join(dir, res.Run.ID)

	jsonBody, err := report.JSON(res.Run)
	if err != nil {
		return "", err
	}
	files := map[string][]byte{
		base + ".json": append(jsonBody, '\n'),
		base + ".md":   []byte(report.ReviewMarkdown(res.Run)),
	}
	message := fmt.Sprintf("chore(review): record findings for %s", res.Run.ID)
	if _, err := gh.PublishFiles(ctx, owner, repo, branch, files, message); err != nil {
		return "", fmt.Errorf("engine: publish findings: %w", err)
	}
	return blobURL(gh.BaseURL, owner, repo, branch, base+".json"), nil
}

func blobURL(apiBase, owner, repo, branch, file string) string {
	htmlBase := strings.Replace(apiBase, "https://api.github.com", "https://github.com", 1)
	return fmt.Sprintf("%s/%s/%s/blob/%s/%s", htmlBase, owner, repo, branch, file)
}
