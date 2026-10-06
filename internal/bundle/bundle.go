// Package bundle is the artifact exchanged between the two phases of a
// fork-safe review: a no-secret collect phase and a secret-bearing finalize
// phase. It carries everything the finalize phase needs without requiring it to
// check out untrusted code.
package bundle

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/reviewers"
)

// Version is the bundle schema version.
const Version = 1

// Bundle is a collected review context.
type Bundle struct {
	Version        int                     `json:"version"`
	Profile        string                  `json:"profile"`
	Repository     string                  `json:"repository,omitempty"`
	Ref            string                  `json:"ref,omitempty"`
	Base           string                  `json:"base,omitempty"`
	Head           string                  `json:"head,omitempty"`
	RunID          string                  `json:"runId"`
	Generated      time.Time               `json:"generated"`
	BlockThreshold string                  `json:"blockThreshold"`
	Rubric         []string                `json:"rubric,omitempty"`
	Files          []reviewers.FileContent `json:"files,omitempty"`
	StaticFindings []findings.Finding      `json:"staticFindings,omitempty"`
}

// Save writes the bundle atomically.
func Save(path string, b Bundle) error {
	if b.Version == 0 {
		b.Version = Version
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("bundle: encode: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("bundle: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("bundle: commit: %w", err)
	}
	return nil
}

// Load reads and validates a bundle.
func Load(path string) (Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Bundle{}, fmt.Errorf("bundle: read: %w", err)
	}
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return Bundle{}, fmt.Errorf("bundle: parse: %w", err)
	}
	if b.Version == 0 {
		return Bundle{}, fmt.Errorf("bundle: missing version")
	}
	return b, nil
}
