// Package scope turns a profile and revision range into the concrete set of
// files to review.
package scope

import (
	"path"
	"strings"

	"github.com/greadee/review-engine/internal/vcs"
)

// Scope is the resolved review input.
type Scope struct {
	Profile string
	Base    string
	Head    string
	Changes []vcs.Change
	// Files are the changed paths selected for review (deleted files excluded).
	Files []string
}

// Resolve selects reviewable files from changes, honouring ignore globs.
// Deleted files are excluded; renamed files are reviewed under their new path.
func Resolve(profile string, changes []vcs.Change, ignore []string) Scope {
	s := Scope{Profile: profile, Changes: changes}
	seen := map[string]bool{}
	for _, c := range changes {
		if c.Status == "D" {
			continue
		}
		if c.Path == "" || seen[c.Path] {
			continue
		}
		if isIgnored(c.Path, ignore) {
			continue
		}
		seen[c.Path] = true
		s.Files = append(s.Files, c.Path)
	}
	return s
}

func isIgnored(p string, patterns []string) bool {
	for _, pattern := range patterns {
		if match(pattern, p) {
			return true
		}
	}
	return false
}

// match supports path.Match globs plus a trailing "/**" directory prefix and a
// leading "**/" any-directory prefix.
func match(pattern, p string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if ok, _ := path.Match(pattern, p); ok {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return p == prefix || strings.HasPrefix(p, prefix+"/")
	}
	if strings.HasPrefix(pattern, "**/") {
		suffix := strings.TrimPrefix(pattern, "**/")
		if base := path.Base(p); func() bool { ok, _ := path.Match(suffix, base); return ok }() {
			return true
		}
	}
	return false
}
