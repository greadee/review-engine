package analyzers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	args           []string
	classification findings.Classification
	severity       findings.Severity
}

// Analyze implements Analyzer. It is a no-op when the directory is not a Go
// module or the Go toolchain is unavailable.
func (g Go) Analyze(ctx context.Context, t Target) ([]findings.Finding, error) {
	if _, err := os.Stat(filepath.Join(t.Dir, "go.mod")); err != nil {
		return nil, nil
	}
	if _, err := exec.LookPath("go"); err != nil {
		return nil, nil
	}
	checks := []check{
		{detector: "static.gofmt", args: []string{"gofmt", "-l", "."}, classification: findings.Maintainability, severity: findings.P3},
		{detector: "static.govet", args: []string{"go", "vet", "./..."}, classification: findings.Bug, severity: findings.P2},
		{detector: "static.gobuild", args: []string{"go", "build", "./..."}, classification: findings.Bug, severity: findings.P1},
	}
	var out []findings.Finding
	for _, c := range checks {
		found, err := runCheck(ctx, t.Dir, c)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	// gofmt reports bare file paths rather than diagnostics.
	gofmtCmd := exec.CommandContext(ctx, "gofmt", "-l", ".")
	gofmtCmd.Dir = t.Dir
	if gofmtOut, err := gofmtCmd.Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(gofmtOut)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			out = append(out, findings.Finding{
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
	}
	return out, nil
}

func runCheck(ctx context.Context, dir string, c check) ([]findings.Finding, error) {
	if c.detector == "static.gofmt" {
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, c.args[0], c.args[1:]...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
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
	return out, nil
}

func cleanPath(p string) string {
	p = strings.TrimPrefix(strings.TrimSpace(p), "./")
	return filepath.ToSlash(p)
}
