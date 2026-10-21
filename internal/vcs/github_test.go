package vcs

import (
	"context"
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

// TestUpsertCommentPostsNewComment proves that UpsertComment always posts a new
// comment without looking up or updating existing ones. Each invocation produces
// an independent comment so that repeated review runs on the same PR preserve
// the full history of findings over time.
func TestUpsertCommentPostsNewComment(t *testing.T) {
	var posted bool
	var postedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
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
	if err := c.UpsertComment(context.Background(), "o", "r", 1, "<!-- revu:run=x -->", "new"); err != nil {
		t.Fatal(err)
	}
	if !posted {
		t.Fatal("expected a new comment to be posted")
	}
	want := "<!-- revu:run=x -->\n\nnew"
	if postedBody != want {
		t.Fatalf("unexpected body: %q", postedBody)
	}
}

// TestUpsertCommentMultipleRunsAccumulate proves that repeated calls on the
// same PR produce multiple independent comments, preserving the history of
// each review run.
func TestUpsertCommentMultipleRunsAccumulate(t *testing.T) {
	var postedCount int
	var postedBodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/42/comments":
			postedCount++
			body, _ := io.ReadAll(r.Body)
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			postedBodies = append(postedBodies, payload["body"])
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewGitHub("token")
	c.BaseURL = srv.URL
	ctx := context.Background()

	// First run: posts a comment.
	if err := c.UpsertComment(ctx, "o", "r", 42, "<!-- revu:pr=42 -->", "first run"); err != nil {
		t.Fatal(err)
	}
	if postedCount != 1 {
		t.Fatalf("expected 1 comment after first run, got %d", postedCount)
	}

	// Second run with a different body but the same PR: posts another comment.
	if err := c.UpsertComment(ctx, "o", "r", 42, "<!-- revu:pr=42 -->", "second run"); err != nil {
		t.Fatal(err)
	}
	if postedCount != 2 {
		t.Fatalf("expected 2 comments after second run, got %d", postedCount)
	}
	if len(postedBodies) != 2 {
		t.Fatalf("expected 2 bodies, got %d", len(postedBodies))
	}
	firstWant := "<!-- revu:pr=42 -->\n\nfirst run"
	secondWant := "<!-- revu:pr=42 -->\n\nsecond run"
	if postedBodies[0] != firstWant {
		t.Fatalf("first body: got %q, want %q", postedBodies[0], firstWant)
	}
	if postedBodies[1] != secondWant {
		t.Fatalf("second body: got %q, want %q", postedBodies[1], secondWant)
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
