package vcs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// GitHub is a minimal REST client for the GitHub API.
type GitHub struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewGitHub builds a client using the token from GITHUB_TOKEN when token is
// empty.
func NewGitHub(token string) *GitHub {
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	return &GitHub{
		BaseURL: "https://api.github.com",
		Token:   token,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// PullRequest is the subset of PR metadata the engine uses.
type PullRequest struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	Base    struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"base"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
}

// PR fetches pull request metadata.
func (c *GitHub) PR(ctx context.Context, owner, repo string, number int) (PullRequest, error) {
	var pr PullRequest
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	if err := c.do(ctx, http.MethodGet, path, nil, &pr); err != nil {
		return PullRequest{}, err
	}
	return pr, nil
}

// Issue is the subset of issue metadata the engine uses.
type Issue struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	Labels  []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// GetIssue fetches issue metadata.
func (c *GitHub) GetIssue(ctx context.Context, owner, repo string, number int) (Issue, error) {
	var issue Issue
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	if err := c.do(ctx, http.MethodGet, path, nil, &issue); err != nil {
		return Issue{}, err
	}
	return issue, nil
}

// SetIssueState opens or closes an issue.
func (c *GitHub) SetIssueState(ctx context.Context, owner, repo string, number int, state string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	return c.do(ctx, http.MethodPatch, path, map[string]string{"state": state}, nil)
}

// Comment posts a comment on an issue or pull request.
func (c *GitHub) Comment(ctx context.Context, owner, repo string, number int, body string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	return c.do(ctx, http.MethodPost, path, map[string]string{"body": body}, nil)
}

// AuthenticatedLogin returns the login of the token's user.
func (c *GitHub) AuthenticatedLogin(ctx context.Context) (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	if err := c.do(ctx, http.MethodGet, "/user", nil, &u); err != nil {
		return "", err
	}
	return u.Login, nil
}

type issueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
}

// UpsertComment posts a comment, or updates this actor's existing comment that
// carries the same hidden marker. It keeps comments idempotent across runs
// (re-runs update rather than duplicate) while leaving comments from other
// authors untouched.
func (c *GitHub) UpsertComment(ctx context.Context, owner, repo string, number int, marker, body string) error {
	login, err := c.AuthenticatedLogin(ctx)
	if err != nil {
		return err
	}
	want := marker + "\n\n" + body
	for page := 1; page <= 10; page++ {
		var comments []issueComment
		path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments?per_page=100&page=%d", owner, repo, number, page)
		if err := c.do(ctx, http.MethodGet, path, nil, &comments); err != nil {
			return err
		}
		for _, cm := range comments {
			if cm.User.Login == login && strings.Contains(cm.Body, marker) {
				return c.do(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/%s/issues/comments/%d", owner, repo, cm.ID), map[string]string{"body": want}, nil)
			}
		}
		if len(comments) < 100 {
			break
		}
	}
	return c.Comment(ctx, owner, repo, number, want)
}

// CreateIssue opens an issue and returns its number.
func (c *GitHub) CreateIssue(ctx context.Context, owner, repo, title, body string, labels []string) (int, error) {
	payload := map[string]any{"title": title, "body": body}
	if len(labels) > 0 {
		payload["labels"] = labels
	}
	var out struct {
		Number int `json:"number"`
	}
	path := fmt.Sprintf("/repos/%s/%s/issues", owner, repo)
	if err := c.do(ctx, http.MethodPost, path, payload, &out); err != nil {
		return 0, err
	}
	return out.Number, nil
}

// PublishFiles commits files to branch, creating the branch from the default
// branch when absent, and returns the branch's new commit SHA. It uses the
// contents API and never touches the default branch.
func (c *GitHub) PublishFiles(ctx context.Context, owner, repo, branch string, files map[string][]byte, message string) (string, error) {
	if c.Token == "" {
		return "", fmt.Errorf("github: no token configured")
	}
	// Ensure the branch exists.
	exists, err := c.refExists(ctx, owner, repo, branch)
	if err != nil {
		return "", err
	}
	if !exists {
		if err := c.createBranch(ctx, owner, repo, branch); err != nil {
			return "", err
		}
	}
	var lastSHA string
	for path, content := range files {
		sha, err := c.putFile(ctx, owner, repo, branch, path, content, message)
		if err != nil {
			return "", err
		}
		lastSHA = sha
	}
	return lastSHA, nil
}

func (c *GitHub) refExists(ctx context.Context, owner, repo, branch string) (bool, error) {
	path := fmt.Sprintf("/repos/%s/%s/git/ref/heads/%s", owner, repo, branch)
	status, _, err := c.raw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return false, err
	}
	switch status {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("github: unexpected status %d checking ref", status)
	}
}

func (c *GitHub) createBranch(ctx context.Context, owner, repo, branch string) error {
	var repoInfo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s", owner, repo), nil, &repoInfo); err != nil {
		return err
	}
	var baseRef struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/git/ref/heads/%s", owner, repo, repoInfo.DefaultBranch), nil, &baseRef); err != nil {
		return err
	}
	payload := map[string]string{"ref": "refs/heads/" + branch, "sha": baseRef.Object.SHA}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/git/refs", owner, repo), payload, nil)
}

// maxContentRetries bounds retries when another writer updates the same file
// concurrently.
const maxContentRetries = 5

func (c *GitHub) putFile(ctx context.Context, owner, repo, branch, path string, content []byte, message string) (string, error) {
	contentsPath := fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path)
	getPath := contentsPath + "?ref=" + branch
	var lastStatus int
	var lastBody string
	for attempt := 0; attempt < maxContentRetries; attempt++ {
		// Read the current blob on the branch so the update carries its sha.
		var existing struct {
			SHA string `json:"sha"`
		}
		status, body, err := c.raw(ctx, http.MethodGet, getPath, nil)
		if err != nil {
			return "", err
		}
		if status == http.StatusOK {
			_ = json.Unmarshal(body, &existing)
		}
		payload := map[string]any{
			"message": message,
			"content": base64.StdEncoding.EncodeToString(content),
			"branch":  branch,
		}
		if existing.SHA != "" {
			payload["sha"] = existing.SHA
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return "", fmt.Errorf("github: encode contents: %w", err)
		}
		st, respBody, err := c.raw(ctx, http.MethodPut, contentsPath, bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		if st >= 200 && st < 300 {
			var out struct {
				Commit struct {
					SHA string `json:"sha"`
				} `json:"commit"`
			}
			_ = json.Unmarshal(respBody, &out)
			return out.Commit.SHA, nil
		}
		// 409/422 mean a concurrent writer changed the file (stale sha) or the
		// branch moved; re-read the current sha and retry.
		if st == http.StatusConflict || st == http.StatusUnprocessableEntity {
			lastStatus, lastBody = st, string(respBody)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
			}
			continue
		}
		return "", fmt.Errorf("github: PUT contents %s: status %d: %s", path, st, truncate(string(respBody), 300))
	}
	return "", fmt.Errorf("github: PUT contents %s: conflict after %d attempts: status %d: %s", path, maxContentRetries, lastStatus, truncate(lastBody, 300))
}

func (c *GitHub) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("github: encode: %w", err)
		}
		body = bytes.NewReader(data)
	}
	const maxRetries = 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		status, respBody, err := c.raw(ctx, method, path, body)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			// Retry on rate limit (429) and server errors (5xx).
			retryable := status == http.StatusTooManyRequests || status >= 500
			if !retryable || attempt == maxRetries {
				return fmt.Errorf("github: %s %s: status %d: %s", method, path, status, truncate(string(respBody), 300))
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 500 * time.Millisecond):
			}
			// Reset body reader for retry (only needed for POST/PUT).
			if in != nil {
				data, _ := json.Marshal(in)
				body = bytes.NewReader(data)
			}
			continue
		}
		if out != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, out); err != nil {
				return fmt.Errorf("github: decode %s: %w", path, err)
			}
		}
		return nil
	}
	return nil
}

func (c *GitHub) raw(ctx context.Context, method, path string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return 0, nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("github: request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("github: read body: %w", err)
	}
	return resp.StatusCode, data, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "..."
}
