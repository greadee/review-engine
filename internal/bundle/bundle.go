// Package bundle is the artifact exchanged between the two phases of a
// fork-safe review: a no-secret collect phase and a secret-bearing finalize
// phase. It carries everything the finalize phase needs without requiring it to
// check out untrusted code.
//
// The bundle is produced by a job that runs PR-controlled code, so the finalize
// phase must treat it as untrusted input. Load enforces an exact schema and
// size limits, and Verify binds it to the trusted workflow event; the engine
// additionally verifies finding evidence against the bundled source.
package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/profiles"
	"github.com/greadee/review-engine/internal/reviewers"
)

// Version is the bundle schema version.
const Version = 1

// Limits bound an untrusted bundle. They are generous enough for real reviews
// but stop a hostile artifact from exhausting memory or the renderer.
const (
	MaxBundleBytes    = 64 << 20 // whole file
	MaxFiles          = 2000
	MaxFileBytes      = 1 << 20 // per file content
	MaxTotalFileBytes = 48 << 20
	MaxFindings       = 2000
	MaxRubric         = 64
	MaxPathLen        = 1024
	MaxRunIDLen       = 256
)

// Bundle is a collected review context.
type Bundle struct {
	Version        int                     `json:"version"`
	Profile        string                  `json:"profile"`
	Repository     string                  `json:"repository,omitempty"`
	PR             int                     `json:"pr,omitempty"`
	Ref            string                  `json:"ref,omitempty"`
	Base           string                  `json:"base,omitempty"`
	Head           string                  `json:"head,omitempty"`
	RunID          string                  `json:"runId"`
	Generated      time.Time               `json:"generated"`
	BlockThreshold string                  `json:"blockThreshold"`
	Rubric         []string                `json:"rubric,omitempty"`
	Files          []reviewers.FileContent `json:"files,omitempty"`
	StaticFindings []findings.Finding      `json:"staticFindings,omitempty"`
	// Coverage describes how the collect phase scoped and gathered the review.
	// It is advisory: the finalize phase also accounts for its own semantic
	// coverage. Values are validated to be sane but cannot be fully trusted.
	Selected       int `json:"selectedFiles,omitempty"`
	Skipped        int `json:"skippedFiles,omitempty"`
	Truncated      int `json:"truncatedFiles,omitempty"`
	TruncatedBytes int `json:"truncatedBytes,omitempty"`
}

// Trusted identifies the review the bundle is expected to describe. Values are
// taken from the trusted workflow event, never from the artifact. Empty fields
// are not checked.
type Trusted struct {
	Repository string
	PR         int
	Head       string
	Profile    string
}

// Verify binds the bundle to the trusted event. A bundle whose repository, PR,
// head revision, or profile does not match the expected values is rejected, so
// a tampered or replayed artifact cannot be finalized as if it were another PR.
func (b Bundle) Verify(t Trusted) error {
	if t.Repository != "" && b.Repository != t.Repository {
		return fmt.Errorf("bundle: repository %q does not match expected %q", b.Repository, t.Repository)
	}
	if t.PR != 0 && b.PR != t.PR {
		return fmt.Errorf("bundle: pull request #%d does not match expected #%d", b.PR, t.PR)
	}
	if t.Head != "" && b.Head != t.Head {
		return fmt.Errorf("bundle: head %q does not match expected %q", b.Head, t.Head)
	}
	if t.Profile != "" && b.Profile != t.Profile {
		return fmt.Errorf("bundle: profile %q does not match expected %q", b.Profile, t.Profile)
	}
	return nil
}

// Validate enforces the bundle schema, field vocabulary, and size limits.
func (b Bundle) Validate() error {
	if b.Version != Version {
		return fmt.Errorf("bundle: unsupported version %d (want %d)", b.Version, Version)
	}
	if !profiles.Valid(b.Profile) {
		return fmt.Errorf("bundle: unknown profile %q", b.Profile)
	}
	if strings.TrimSpace(b.RunID) == "" || len(b.RunID) > MaxRunIDLen {
		return fmt.Errorf("bundle: invalid runId")
	}
	if b.BlockThreshold != "" && !findings.Severity(strings.ToUpper(b.BlockThreshold)).Valid() {
		return fmt.Errorf("bundle: invalid blockThreshold %q", b.BlockThreshold)
	}
	if len(b.Rubric) > MaxRubric {
		return fmt.Errorf("bundle: too many rubric entries (%d)", len(b.Rubric))
	}
	for _, r := range b.Rubric {
		if len(r) > 256 {
			return fmt.Errorf("bundle: rubric entry too long")
		}
	}
	if len(b.Files) > MaxFiles {
		return fmt.Errorf("bundle: too many files (%d > %d)", len(b.Files), MaxFiles)
	}
	seen := make(map[string]bool, len(b.Files))
	total := 0
	for _, f := range b.Files {
		if err := validatePath(f.Path); err != nil {
			return err
		}
		if seen[strings.ToLower(f.Path)] {
			return fmt.Errorf("bundle: duplicate file %q", f.Path)
		}
		seen[strings.ToLower(f.Path)] = true
		if len(f.Content) > MaxFileBytes {
			return fmt.Errorf("bundle: file %q exceeds %d bytes", f.Path, MaxFileBytes)
		}
		total += len(f.Content)
		if total > MaxTotalFileBytes {
			return fmt.Errorf("bundle: total file content exceeds %d bytes", MaxTotalFileBytes)
		}
	}
	if len(b.StaticFindings) > MaxFindings {
		return fmt.Errorf("bundle: too many findings (%d)", len(b.StaticFindings))
	}
	for i, f := range b.StaticFindings {
		if err := f.Validate(); err != nil {
			return fmt.Errorf("bundle: static finding %d: %w", i, err)
		}
	}
	if b.Selected < 0 || b.Skipped < 0 || b.Truncated < 0 || b.TruncatedBytes < 0 {
		return fmt.Errorf("bundle: coverage counts must be non-negative")
	}
	if b.Selected != 0 && b.Selected < len(b.Files) {
		return fmt.Errorf("bundle: selectedFiles (%d) is less than the file count (%d)", b.Selected, len(b.Files))
	}
	return nil
}

func validatePath(p string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("bundle: empty file path")
	}
	if len(p) > MaxPathLen {
		return fmt.Errorf("bundle: file path too long")
	}
	slash := strings.ReplaceAll(p, "\\", "/")
	if strings.HasPrefix(slash, "/") {
		return fmt.Errorf("bundle: absolute file path %q", p)
	}
	for _, part := range strings.Split(slash, "/") {
		if part == ".." {
			return fmt.Errorf("bundle: file path escapes the repository: %q", p)
		}
	}
	if strings.ContainsAny(p, "\x00\n\r") {
		return fmt.Errorf("bundle: file path contains control characters")
	}
	return nil
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

// Load reads and validates a bundle. It refuses files larger than MaxBundleBytes
// before decoding, and enforces the schema via Validate.
func Load(path string) (Bundle, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Bundle{}, fmt.Errorf("bundle: read: %w", err)
	}
	if info.Size() > MaxBundleBytes {
		return Bundle{}, fmt.Errorf("bundle: file is %d bytes (max %d)", info.Size(), MaxBundleBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Bundle{}, fmt.Errorf("bundle: read: %w", err)
	}
	if int64(len(data)) > MaxBundleBytes {
		return Bundle{}, fmt.Errorf("bundle: file is larger than %d bytes", MaxBundleBytes)
	}
	var b Bundle
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return Bundle{}, fmt.Errorf("bundle: parse: %w", err)
	}
	if err := b.Validate(); err != nil {
		return Bundle{}, err
	}
	return b, nil
}
