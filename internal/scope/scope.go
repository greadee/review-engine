// Package scope turns a profile and revision range into the concrete set of
// files to review.
package scope

import (
	"path"
	"sort"
	"strings"

	"github.com/greadee/review-engine/internal/profiles"
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

// ChangesForProfile chooses the trusted diff for a profile. PR uses the merge
// base so a moved base branch does not add reverse changes. Sprint and impact
// use the direct two-dot diff of their requested range; audit has no diff and
// enumerates the repository instead.
func ChangesForProfile(repo *vcs.Repo, profile, base, head string) ([]vcs.Change, error) {
	if profile == profiles.PR {
		return repo.ChangesFromMergeBase(base, head)
	}
	return repo.Changes(base, head)
}

// ResolveImpact expands an impact review to the touched subsystems: the
// changed files plus every head file under the directory containing a changed
// file. Root-level files are included only when they changed themselves.
func ResolveImpact(repo *vcs.Repo, head string, changes []vcs.Change, ignore []string) Scope {
	s := Resolve(profiles.Impact, changes, ignore)
	if len(s.Files) == 0 || repo == nil {
		return s
	}
	dirs := map[string]bool{}
	changed := map[string]bool{}
	for _, f := range s.Files {
		changed[f] = true
		if d := path.Dir(f); d != "." {
			dirs[d] = true
		}
	}
	all, err := repo.ListFiles(head)
	if err != nil {
		return s
	}
	seen := map[string]bool{}
	var files []string
	for _, f := range all {
		if f == "" || seen[f] || isIgnored(f, ignore) {
			continue
		}
		include := changed[f]
		if !include {
			for d := path.Dir(f); d != "." && d != "/"; d = path.Dir(d) {
				if dirs[d] {
					include = true
					break
				}
			}
		}
		if include {
			seen[f] = true
			files = append(files, f)
		}
	}
	sort.Strings(files)
	s.Files = files
	return s
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

// FilterPaths applies ignore globs to a whole-repo file list. It preserves
// input order, removes duplicates, and never returns an empty path.
func FilterPaths(paths, ignore []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range paths {
		if p == "" || seen[p] || isIgnored(p, ignore) {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
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
