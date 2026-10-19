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

func TestUpsertCommentUpdatesOwn(t *testing.T) {
	var patched, posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/user":
			_, _ = w.Write([]byte(`{"login":"bot"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/1/comments":
			_, _ = w.Write([]byte(`[{"id":7,"body":"<!-- revu:run=x -->\nold","user":{"login":"bot"}}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/o/r/issues/comments/7":
			patched = true
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/1/comments":
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
	if !patched || posted {
		t.Fatalf("expected in-place update: patched=%v posted=%v", patched, posted)
	}
}

func TestUpsertCommentCreatesWhenNone(t *testing.T) {
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/user":
			_, _ = w.Write([]byte(`{"login":"bot"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/1/comments":
			_, _ = w.Write([]byte(`[{"id":9,"body":"unrelated","user":{"login":"someone"}}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/1/comments":
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
		t.Fatal("expected a new comment when no own marker exists")
	}
}

// TestUpsertCommentStableMarkerAcrossRuns proves that re-runs on the same PR
// update the existing comment rather than accumulating new ones: the marker
// is stable across runs with different run IDs.
func TestUpsertCommentStableMarkerAcrossRuns(t *testing.T) {
	var posted, patched bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/user":
			_, _ = w.Write([]byte(`{"login":"bot"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/42/comments":
			// First call: no existing comment with the stable marker.
			if !posted {
				_, _ = w.Write([]byte(`[]`))
				break
			}
			// Second call: the first run already posted with the stable marker.
			_, _ = w.Write([]byte(`[{"id":55,"body":"<!-- revu:pr=42 -->\nfirst run","user":{"login":"bot"}}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/o/r/issues/comments/55":
			patched = true
			_, _ = w.Write([]byte(`{}`))
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
	ctx := context.Background()

	// First run: no prior comment, should create one.
	if err := c.UpsertComment(ctx, "o", "r", 42, "<!-- revu:pr=42 -->", "first run"); err != nil {
		t.Fatal(err)
	}
	if !posted {
		t.Fatal("expected first run to create a comment")
	}

	// Second run with a different run ID but the same PR: should update, not duplicate.
	if err := c.UpsertComment(ctx, "o", "r", 42, "<!-- revu:pr=42 -->", "second run"); err != nil {
		t.Fatal(err)
	}
	if !patched {
		t.Fatal("expected second run to patch the existing comment, not create a duplicate")
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
