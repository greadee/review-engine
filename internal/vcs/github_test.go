package vcs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
