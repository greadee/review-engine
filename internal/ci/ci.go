// Package ci detects the continuous-integration environment so the engine can
// infer revisions and the pull request number without per-repository flags.
package ci

import (
	"encoding/json"
	"os"
)

// Info is the detected CI context.
type Info struct {
	Detected   bool
	Provider   string
	Base       string
	Head       string
	Repository string
	Event      string
	PR         int
}

// Detect inspects the environment. It currently understands GitHub Actions and
// is a no-op elsewhere.
func Detect() Info {
	return detectFrom(os.Getenv)
}

func detectFrom(getenv func(string) string) Info {
	var info Info
	if getenv("GITHUB_ACTIONS") != "true" {
		return info
	}
	info.Detected = true
	info.Provider = "github-actions"
	if ref := getenv("GITHUB_BASE_REF"); ref != "" {
		info.Base = "origin/" + ref
	}
	info.Head = getenv("GITHUB_SHA")
	info.Repository = getenv("GITHUB_REPOSITORY")
	info.Event = getenv("GITHUB_EVENT_NAME")
	info.PR = prNumber(getenv("GITHUB_EVENT_PATH"))
	return info
}

func prNumber(path string) int {
	if path == "" {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var ev struct {
		Number      int `json:"number"`
		PullRequest struct {
			Number int `json:"number"`
		} `json:"pull_request"`
		Issue struct {
			Number int `json:"number"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return 0
	}
	switch {
	case ev.PullRequest.Number > 0:
		return ev.PullRequest.Number
	case ev.Issue.Number > 0:
		return ev.Issue.Number
	default:
		return ev.Number
	}
}
