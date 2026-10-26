package vcs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubCreateIssue(t *testing.T) {
	var gotTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues" {
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			_ = json.Unmarshal(body, &payload)
			gotTitle, _ = payload["title"].(string)
			_, _ = w.Write([]byte(`{"number":123}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewGitHub("token")
	c.BaseURL = srv.URL
	num, err := c.CreateIssue(context.Background(), "o", "r", "[revu] orphan (2 finding(s))", "body", []string{"revu", "P2"})
	if err != nil {
		t.Fatal(err)
	}
	if num != 123 || gotTitle != "[revu] orphan (2 finding(s))" {
		t.Fatalf("unexpected: num=%d title=%q", num, gotTitle)
	}
}

func TestPutFileRetriesOnConflict(t *testing.T) {
	puts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/o/r/contents/x.json"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && r.URL.Path == "/repos/o/r/contents/x.json":
			puts++
			if puts == 1 {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"message":"is at abc but expected def"}`))
				return
			}
			_, _ = w.Write([]byte(`{"commit":{"sha":"newsha"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewGitHub("token")
	c.BaseURL = srv.URL
	sha, err := c.putFile(context.Background(), "o", "r", "b", "x.json", []byte("{}"), "m")
	if err != nil {
		t.Fatalf("putFile: %v", err)
	}
	if sha != "newsha" || puts != 2 {
		t.Fatalf("sha=%q puts=%d", sha, puts)
	}
}

// TestUpsertCommentPostsNewComment verifies that a PR without a marker
// comment gets one summary comment.
func TestUpsertCommentPostsNewComment(t *testing.T) {
	var posted bool
	var postedBody string
	var listed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/1/comments":
			listed = true
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/1/comments":
			body, _ := io.ReadAll(r.Body)
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			postedBody = payload["body"]
			posted = true
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewGitHub("token")
	c.BaseURL = srv.URL
	if err := c.UpsertComment(context.Background(), "o", "r", 1, "<!-- revu:pr=1 -->", "new"); err != nil {
		t.Fatal(err)
	}
	if !listed || !posted {
		t.Fatalf("expected list then post, listed=%v posted=%v", listed, posted)
	}
	want := "<!-- revu:pr=1 -->\n\nnew"
	if postedBody != want {
		t.Fatalf("unexpected body: %q", postedBody)
	}
}

// TestUpsertCommentUpdatesExistingComment verifies the stable-comment contract:
// a rerun updates the marker comment in place and does not post a duplicate,
// without calling GET /user.
func TestUpsertCommentUpdatesExistingComment(t *testing.T) {
	var listed, patched, posted bool
	var patchedBody string
	var userLookedUp bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/user":
			userLookedUp = true
			w.WriteHeader(http.StatusForbidden)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/42/comments":
			listed = true
			_, _ = w.Write([]byte(`[{"id":9,"body":"<!-- revu:pr=42 -->\n\nfirst run"}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/o/r/issues/comments/9":
			patched = true
			body, _ := io.ReadAll(r.Body)
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			patchedBody = payload["body"]
			_, _ = w.Write([]byte(`{"id":9}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/42/comments":
			posted = true
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewGitHub("token")
	c.BaseURL = srv.URL
	if err := c.UpsertComment(context.Background(), "o", "r", 42, "<!-- revu:pr=42 -->", "second run"); err != nil {
		t.Fatal(err)
	}
	if !listed || !patched || posted {
		t.Fatalf("expected list and patch, not post: listed=%v patched=%v posted=%v", listed, patched, posted)
	}
	if userLookedUp {
		t.Fatal("UpsertComment must not call GET /user")
	}
	want := "<!-- revu:pr=42 -->\n\nsecond run"
	if patchedBody != want {
		t.Fatalf("patched body = %q, want %q", patchedBody, want)
	}
}

func TestDoRetriesOnRateLimitAndServerErrors(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			if r.URL.Path == "/rate-limited" {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"message":"rate limited"}`))
				return
			}
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"message":"server error"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewGitHub("token")
	c.BaseURL = srv.URL

	// Rate limit retry.
	err := c.do(context.Background(), http.MethodGet, "/rate-limited", nil, nil)
	if err != nil {
		t.Fatalf("rate limit retry failed: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls for rate limit retry, got %d", calls)
	}

	// Server error retry.
	calls = 0
	err = c.do(context.Background(), http.MethodGet, "/server-error", nil, nil)
	if err != nil {
		t.Fatalf("server error retry failed: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls for server error retry, got %d", calls)
	}
}

func TestNewGitHubUsesConfiguredToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"login":"bot"}`))
	}))
	defer srv.Close()

	c := NewGitHub("custom-token")
	c.BaseURL = srv.URL
	login, err := c.AuthenticatedLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if login != "bot" {
		t.Fatalf("expected login=bot, got %q", login)
	}
	if gotAuth != "Bearer custom-token" {
		t.Fatalf("expected Authorization: Bearer custom-token, got %q", gotAuth)
	}
}

func TestGitHubCommentAndPublish(t *testing.T) {
	var commented string
	var branchCreated bool
	var putPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/7/comments":
			body, _ := io.ReadAll(r.Body)
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			commented = payload["body"]
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/git/ref/heads/review-artifacts":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r":
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/git/ref/heads/main":
			_, _ = w.Write([]byte(`{"object":{"sha":"basesha"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/git/refs":
			branchCreated = true
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/contents/runs/x.json":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && r.URL.Path == "/repos/o/r/contents/runs/x.json":
			putPath = r.URL.Path
			_, _ = w.Write([]byte(`{"commit":{"sha":"newsha"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewGitHub("token")
	c.BaseURL = srv.URL
	ctx := context.Background()

	if err := c.Comment(ctx, "o", "r", 7, "hello"); err != nil {
		t.Fatal(err)
	}
	if commented != "hello" {
		t.Fatalf("comment not posted: %q", commented)
	}

	sha, err := c.PublishFiles(ctx, "o", "r", "review-artifacts", map[string][]byte{"runs/x.json": []byte("{}")}, "msg")
	if err != nil {
		t.Fatal(err)
	}
	if !branchCreated || putPath != "/repos/o/r/contents/runs/x.json" || sha == "" {
		t.Fatalf("publish failed: branch=%v path=%q sha=%q", branchCreated, putPath, sha)
	}
}

func TestGitHubPullRequestFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/repos/o/r/pulls/7/files" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`[{"filename":"a.go","status":"modified"},{"filename":"b.go","status":"removed"}]`))
	}))
	defer srv.Close()

	c := NewGitHub("token")
	c.BaseURL = srv.URL
	files, err := c.PullRequestFiles(context.Background(), "o", "r", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Filename != "a.go" || files[1].Status != "removed" {
		t.Fatalf("unexpected files: %+v", files)
	}
}

func TestGitHubFileAtRef(t *testing.T) {
	content := base64.StdEncoding.EncodeToString([]byte("ignore:\n  - docs/**\n"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/contents/revu.yaml":
			if r.URL.Query().Get("ref") != "base-sha" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"content":"` + content + `","encoding":"base64"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewGitHub("token")
	c.BaseURL = srv.URL
	data, found, err := c.FileAtRef(context.Background(), "o", "r", "revu.yaml", "base-sha")
	if err != nil {
		t.Fatal(err)
	}
	if !found || string(data) != "ignore:\n  - docs/**\n" {
		t.Fatalf("unexpected file: found=%v data=%q", found, data)
	}
	if _, found, err := c.FileAtRef(context.Background(), "o", "r", "revu.yaml", "missing"); err != nil || found {
		t.Fatalf("missing file should be found=false: found=%v err=%v", found, err)
	}
}

func TestGitHubDefaultBranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/repos/o/r" {
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewGitHub("token")
	c.BaseURL = srv.URL
	branch, err := c.DefaultBranch(context.Background(), "o", "r")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Fatalf("branch = %q", branch)
	}
}
