// Package refs verifies that every documented reference to this action is
// pinned to an immutable, reviewed revision, so consumers cannot be silently
// upgraded when this repository changes.
package refs

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// allowedTags are maintained moving tags that may appear in documentation. Their
// existence is verified against the remote by CI (a network check), not here.
var allowedTags = map[string]bool{"v1": true}

var (
	actionRef = regexp.MustCompile(`greadee/review-engine(?:/[^\s@]+)?@([A-Za-z0-9_./-]+)`)
	fullSHA   = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

func repoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join("..", ".."))
}

func refFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	walk := func(rel string) {
		full := filepath.Join(root, rel)
		info, err := os.Stat(full)
		if err != nil {
			return
		}
		if !info.IsDir() {
			out = append(out, full)
			return
		}
		_ = filepath.Walk(full, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			switch filepath.Ext(p) {
			case ".md", ".yml", ".yaml":
				out = append(out, p)
			}
			return nil
		})
	}
	walk("README.md")
	for _, dir := range []string{"docs", "examples", ".github"} {
		walk(dir)
	}
	return out
}

// TestActionReferencesPinned scans documentation, examples, and workflows for
// references to this action and requires each to be a full commit SHA (which
// must exist and be an ancestor of HEAD) or an allowed maintained tag.
func TestActionReferencesPinned(t *testing.T) {
	root := repoRoot(t)
	files := refFiles(t, root)
	if len(files) == 0 {
		t.Fatal("no reference files found")
	}
	found := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			for _, m := range actionRef.FindAllStringSubmatch(line, -1) {
				found++
				ref := m[1]
				switch {
				case fullSHA.MatchString(ref):
					if err := checkSHA(root, ref); err != nil {
						t.Errorf("%s:%d pins to %s: %v", file, i+1, ref, err)
					}
				case allowedTags[ref]:
					// remote existence verified by CI
				default:
					t.Errorf("%s:%d: action reference @%s must be a full commit SHA or an allowed tag (%s)",
						file, i+1, ref, strings.Join(tagNames(), ", "))
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no action references found (scan likely broken)")
	}
}

func checkSHA(root, sha string) error {
	if out, err := exec.Command("git", "-C", root, "cat-file", "-e", sha+"^{commit}").CombinedOutput(); err != nil {
		return &refError{msg: "does not resolve to a commit: " + strings.TrimSpace(string(out))}
	}
	if out, err := exec.Command("git", "-C", root, "merge-base", "--is-ancestor", sha, "HEAD").CombinedOutput(); err != nil {
		return &refError{msg: "is not an ancestor of HEAD: " + strings.TrimSpace(string(out))}
	}
	return nil
}

type refError struct{ msg string }

func (e *refError) Error() string { return e.msg }

func tagNames() []string {
	out := make([]string, 0, len(allowedTags))
	for k := range allowedTags {
		out = append(out, k)
	}
	return out
}

var thirdPartyAction = regexp.MustCompile(`uses:\s*(actions/[A-Za-z0-9_.-]+)@([^\s#]+)`)

func TestThirdPartyActionsPinned(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "action.yml"),
		filepath.Join(root, "README.md"),
	}
	for _, dir := range []string{".github", "examples"} {
		_ = filepath.Walk(filepath.Join(root, dir), func(p string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() && (filepath.Ext(p) == ".yml" || filepath.Ext(p) == ".yaml") {
				paths = append(paths, p)
			}
			return nil
		})
	}
	found := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, m := range thirdPartyAction.FindAllStringSubmatch(line, -1) {
				found++
				if !fullSHA.MatchString(m[2]) {
					t.Errorf("%s:%d: %s is not pinned to a full commit SHA", path, i+1, m[1])
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no third-party action references found (scan likely broken)")
	}
}
