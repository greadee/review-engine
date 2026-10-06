package ci

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectGitHubActions(t *testing.T) {
	event := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(event, []byte(`{"pull_request":{"number":42}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_BASE_REF":   "main",
		"GITHUB_SHA":        "abc123",
		"GITHUB_REPOSITORY": "owner/repo",
		"GITHUB_EVENT_NAME": "pull_request",
		"GITHUB_EVENT_PATH": event,
	}
	got := detectFrom(func(k string) string { return env[k] })
	if !got.Detected || got.Provider != "github-actions" {
		t.Fatalf("not detected: %+v", got)
	}
	if got.Base != "origin/main" || got.Head != "abc123" || got.Repository != "owner/repo" || got.PR != 42 {
		t.Fatalf("unexpected info: %+v", got)
	}
}

func TestDetectEventFallbacks(t *testing.T) {
	event := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(event, []byte(`{"issue":{"number":7}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := detectFrom(func(k string) string {
		if k == "GITHUB_ACTIONS" {
			return "true"
		}
		if k == "GITHUB_EVENT_PATH" {
			return event
		}
		return ""
	})
	if got.PR != 7 {
		t.Fatalf("expected issue PR fallback 7, got %d", got.PR)
	}
}

func TestDetectNone(t *testing.T) {
	if got := detectFrom(func(string) string { return "" }); got.Detected {
		t.Fatalf("expected no detection: %+v", got)
	}
}
