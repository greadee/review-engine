package analyzers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/greadee/review-engine/internal/findings"
)

// Go is the static analyzer for Go repositories.
type Go struct{}

// NewGo returns a Go analyzer.
func NewGo() *Go { return &Go{} }

// Name implements Analyzer.
func (Go) Name() string { return "go" }

var goDiag = regexp.MustCompile(`^(.+?\.go):(\d+)(?::\d+)?:\s*(.+)$`)

type check struct {
	detector       string
	subcommand     string
	classification findings.Classification
	severity       findings.Severity
}

// Analyze implements Analyzer. It is a no-op when the directory is not a Go
// module. When the module is present but the Go toolchain is missing, it
// returns ErrUnavailable so the run reports an analyzer coverage gap instead of
// silently approving.
func (g Go) Analyze(ctx context.Context, t Target) ([]findings.Finding, error) {
	if _, err := os.Stat(filepath.Join(t.Dir, "go.mod")); err != nil {
		return nil, nil
	}
	if _, err := exec.LookPath("go"); err != nil {
		return nil, unavailable("go")
	}

	packages := goPackages(t.Files, t.WholeRepo)
	var out []findings.Finding
	checks := []check{
		{detector: "static.govet", subcommand: "vet", classification: findings.Bug, severity: findings.P2},
		{detector: "static.gobuild", subcommand: "build", classification: findings.Bug, severity: findings.P1},
	}
	for _, c := range checks {
		if len(packages) == 0 {
			break
		}
		found, err := runCheck(ctx, t.Dir, c, packages)
		out = append(out, found...)
		if err != nil {
			return out, err
		}
	}

	goFiles := filterExt(t.Files, ".go")
	if len(goFiles) > 0 {
		found, err := runGofmt(ctx, t.Dir, goFiles)
		out = append(out, found...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// goPackages maps changed Go files to package arguments. PR/impact/issue scans
// stay scoped to the packages they touch; whole-repo profiles use ./... .
func goPackages(files []string, wholeRepo bool) []string {
	if wholeRepo {
		return []string{"./..."}
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		dir := path.Dir(cleanPath(f))
		pkg := "."
		if dir != "." {
			pkg = "./" + dir
		}
		if !seen[pkg] {
			seen[pkg] = true
			out = append(out, pkg)
		}
	}
	sort.Strings(out)
	return out
}

func runCheck(ctx context.Context, dir string, c check, packages []string) ([]findings.Finding, error) {
	args := append([]string{c.subcommand}, packages...)
	cmdCtx, cancel := withAnalyzerTimeout(ctx)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "go", args...)
	cmd.Dir = dir
	cmd.Env = childEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if cmdCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("%s timed out after %s", c.detector, analyzerTimeout)
	}
	if err == nil {
		return nil, nil
	}
	combined := stdout.String() + stderr.String()
	var out []findings.Finding
	for _, line := range strings.Split(strings.TrimSpace(combined), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := goDiag.FindStringSubmatch(line)
		f := findings.Finding{
			Classification: c.classification,
			Severity:       c.severity,
			Detector:       c.detector,
		}
		if m != nil {
			lineNo, _ := strconv.Atoi(m[2])
			f.Title = fmt.Sprintf("%s: %s", c.detector, m[3])
			f.Evidence = findings.Evidence{File: cleanPath(m[1]), Line: lineNo, Snippet: m[3]}
			f.Anchor = m[3]
		} else {
			f.Title = fmt.Sprintf("%s: %s", c.detector, line)
			f.Evidence = findings.Evidence{Snippet: line}
			f.Anchor = line
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s failed: %w: %s", c.detector, err, strings.TrimSpace(combined))
	}
	return out, nil
}

func runGofmt(ctx context.Context, dir string, files []string) ([]findings.Finding, error) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		return nil, unavailable("gofmt")
	}
	args := append([]string{"-l", "--"}, files...)
	cmdCtx, cancel := withAnalyzerTimeout(ctx)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "gofmt", args...)
	cmd.Dir = dir
	cmd.Env = childEnv()
	out, err := cmd.Output()
	if cmdCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("static.gofmt timed out after %s", analyzerTimeout)
	}
	if err != nil {
		return nil, fmt.Errorf("static.gofmt: %w", err)
	}
	var found []findings.Finding
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		found = append(found, findings.Finding{
			Title:          "File is not gofmt-formatted",
			Classification: findings.Maintainability,
			Severity:       findings.P3,
			Evidence:       findings.Evidence{File: cleanPath(line)},
			Impact:         "Formatting drift; may fail CI format gates.",
			Recommendation: "Run gofmt -w on the file.",
			Detector:       "static.gofmt",
			Anchor:         "gofmt",
		})
	}
	return found, nil
}

func cleanPath(p string) string {
	p = strings.TrimPrefix(strings.TrimSpace(p), "./")
	return filepath.ToSlash(p)
}
