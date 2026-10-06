// Package vcs provides access to a Git working tree (local) and the GitHub
// REST API (remote) for ingesting review inputs.
package vcs

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Repo is a local Git repository.
type Repo struct {
	Dir string
}

// Open returns a Repo rooted at dir.
func Open(dir string) (*Repo, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	r := &Repo{Dir: dir}
	if _, err := r.run("rev-parse", "--git-dir"); err != nil {
		return nil, fmt.Errorf("vcs: %s is not a git repository: %w", dir, err)
	}
	return r, nil
}

// Change describes one file change between two revisions.
type Change struct {
	Status  string // A, M, D, R, C
	Path    string
	OldPath string
}

// HeadSHA returns the commit SHA for ref (or HEAD when ref is empty).
func (r *Repo) HeadSHA(ref string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	out, err := r.run("rev-parse", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// MergeBase returns the merge base of a and b.
func (r *Repo) MergeBase(a, b string) (string, error) {
	out, err := r.run("merge-base", a, b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Changes lists changed files between base and head, following renames.
func (r *Repo) Changes(base, head string) ([]Change, error) {
	out, err := r.run("diff", "--name-status", "-M", base, head)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

func parseNameStatus(out string) []Change {
	var changes []Change
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := string(fields[0][0])
		switch status {
		case "R", "C":
			if len(fields) >= 3 {
				changes = append(changes, Change{Status: status, OldPath: fields[1], Path: fields[2]})
			}
		default:
			changes = append(changes, Change{Status: status, Path: fields[1]})
		}
	}
	return changes
}

// File returns the contents of path at ref. When ref is empty, the working
// tree is used.
func (r *Repo) File(ref, path string) ([]byte, error) {
	if ref == "" {
		return r.runBytes("show", ":"+path)
	}
	return r.runBytes("show", ref+":"+path)
}

// ListFiles returns all tracked files at ref.
func (r *Repo) ListFiles(ref string) ([]string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	out, err := r.run("ls-tree", "-r", "--name-only", ref)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func (r *Repo) run(args ...string) (string, error) {
	out, err := r.runBytes(args...)
	return string(out), err
}

func (r *Repo) runBytes(args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", r.Dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
