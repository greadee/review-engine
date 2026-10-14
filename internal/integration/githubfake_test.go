package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeGitHub is a minimal in-memory GitHub REST server covering the endpoints
// the engine uses to comment and to archive findings. It lets the integration
// harness assert idempotent comments, an immutable archive link, and
// read/write behavior without a real repository.
type fakeGitHub struct {
	mu            sync.Mutex
	comments      []fakeComment
	nextCommentID int64
	branchExists  bool
	contents      map[string][]byte
	commitSeq     int
}

type fakeComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
}

func newFakeGitHub() *fakeGitHub {
	return &fakeGitHub{contents: map[string][]byte{}, nextCommentID: 1}
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/user":
		writeJSON(w, 200, map[string]string{"login": "revu-bot"})

	case r.Method == http.MethodGet && strings.HasSuffix(path, "/issues/42/comments"):
		writeJSON(w, 200, f.comments)

	case r.Method == http.MethodPost && strings.HasSuffix(path, "/issues/42/comments"):
		var in struct {
			Body string `json:"body"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		c := fakeComment{ID: f.nextCommentID, Body: in.Body}
		c.User.Login = "revu-bot"
		f.nextCommentID++
		f.comments = append(f.comments, c)
		writeJSON(w, 201, c)

	case r.Method == http.MethodPatch && strings.HasPrefix(path, "/repos/o/r/issues/comments/"):
		var in struct {
			Body string `json:"body"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		for i := range f.comments {
			if fmt.Sprintf("%d", f.comments[i].ID) == path[strings.LastIndex(path, "/")+1:] {
				f.comments[i].Body = in.Body
				writeJSON(w, 200, f.comments[i])
				return
			}
		}
		writeJSON(w, 404, map[string]string{"message": "not found"})

	case r.Method == http.MethodGet && strings.HasSuffix(path, "/git/ref/heads/review-artifacts"):
		if f.branchExists {
			writeJSON(w, 200, map[string]any{"ref": "refs/heads/review-artifacts"})
		} else {
			writeJSON(w, 404, map[string]string{"message": "not found"})
		}

	case r.Method == http.MethodPost && strings.HasSuffix(path, "/git/refs"):
		f.branchExists = true
		writeJSON(w, 201, map[string]any{"ref": "refs/heads/review-artifacts"})

	case r.Method == http.MethodGet && strings.HasSuffix(path, "/git/ref/heads/main"):
		writeJSON(w, 200, map[string]any{"object": map[string]string{"sha": "basesha0000000000000000000000000000000000"}})

	case r.Method == http.MethodGet && path == "/repos/o/r":
		writeJSON(w, 200, map[string]string{"default_branch": "main"})

	case strings.HasPrefix(path, "/repos/o/r/contents/"):
		file := strings.TrimPrefix(path, "/repos/o/r/contents/")
		switch r.Method {
		case http.MethodGet:
			if data, ok := f.contents[file]; ok {
				writeJSON(w, 200, map[string]string{"sha": "blob-" + file, "content": string(data)})
				return
			}
			writeJSON(w, 404, map[string]string{"message": "not found"})
		case http.MethodPut:
			var in struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.contents[file] = []byte(in.Content)
			f.commitSeq++
			writeJSON(w, 200, map[string]any{"commit": map[string]string{"sha": fmt.Sprintf("commit%040d", f.commitSeq)}})
		default:
			writeJSON(w, 405, map[string]string{"message": "method not allowed"})
		}

	default:
		writeJSON(w, 404, map[string]string{"message": "unhandled " + r.Method + " " + path})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeGitHub) commentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.comments)
}

func (f *fakeGitHub) commentBody(i int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i < 0 || i >= len(f.comments) {
		return ""
	}
	return f.comments[i].Body
}

func (f *fakeGitHub) files() map[string][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string][]byte, len(f.contents))
	for k, v := range f.contents {
		out[k] = v
	}
	return out
}

// newFakeServer returns a running server and its URL.
func newFakeServer(t *testing.T) (*fakeGitHub, string) {
	t.Helper()
	f := newFakeGitHub()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}
