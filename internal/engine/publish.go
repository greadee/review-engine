package engine

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/greadee/review-engine/internal/profiles"
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
	return blobURL(gh.BaseURL, owner, repo, branch, base+".md"), nil
}

func blobURL(apiBase, owner, repo, branch, file string) string {
	htmlBase := strings.Replace(apiBase, "https://api.github.com", "https://github.com", 1)
	return fmt.Sprintf("%s/%s/%s/blob/%s/%s", htmlBase, owner, repo, branch, file)
}

// PublishLocal writes the run to a local, project-scoped directory and never
// touches GitHub. The layout mirrors the remote archive so the local store is a
// drop-in: <dir>/<project>/<archive.dir>/<runID>.json and .md. It returns the
// path of the Markdown summary.
func (e *Engine) PublishLocal(dir, project string, res Result) (string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = e.opts.Config.Local.Dir
	}
	if strings.TrimSpace(dir) == "" {
		dir = ".review-engine"
	}
	archiveDir := e.opts.Config.Archive.Dir
	if archiveDir == "" {
		archiveDir = "runs"
	}
	base := filepath.Join(dir, projectDir(project), archiveDir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("engine: local storage: %w", err)
	}
	jsonBody, err := report.JSON(res.Run)
	if err != nil {
		return "", err
	}
	runBase := filepath.Join(base, res.Run.ID)
	if err := os.WriteFile(runBase+".json", append(jsonBody, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("engine: write local findings: %w", err)
	}
	md := report.ReviewMarkdown(res.Run)
	if res.Run.Profile == profiles.Audit || res.Run.Profile == profiles.Sprint {
		md = report.AuditMarkdown(res.Run)
	}
	if err := os.WriteFile(runBase+".md", []byte(md), 0o644); err != nil {
		return "", fmt.Errorf("engine: write local summary: %w", err)
	}
	return runBase + ".md", nil
}

// projectDir sanitizes a project name into a single directory segment.
func projectDir(project string) string {
	project = strings.TrimSpace(project)
	project = strings.ReplaceAll(project, "/", "-")
	project = strings.ReplaceAll(project, "\\", "-")
	if project == "" {
		return "project"
	}
	return project
}
