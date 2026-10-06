package analyzers

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/greadee/review-engine/internal/findings"
)

// TypeScript is the static analyzer for TypeScript projects. It runs the
// project's TypeScript compiler in no-emit mode when one is available.
type TypeScript struct{}

// NewTypeScript returns a TypeScript analyzer.
func NewTypeScript() *TypeScript { return &TypeScript{} }

// Name implements Analyzer.
func (TypeScript) Name() string { return "typescript" }

var tscDiag = regexp.MustCompile(`^(.+?)\((\d+),(\d+)\):\s*error\s+(TS\d+):\s*(.+)$`)

// Analyze implements Analyzer. It is a no-op without a tsconfig.json or a
// TypeScript compiler.
func (ts TypeScript) Analyze(ctx context.Context, t Target) ([]findings.Finding, error) {
	if _, err := os.Stat(filepath.Join(t.Dir, "tsconfig.json")); err != nil {
		return nil, nil
	}
	if len(filterExt(t.Files, ".ts"))+len(filterExt(t.Files, ".tsx")) == 0 {
		return nil, nil
	}
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, tsc, "--noEmit", "--pretty", "false")
	cmd.Dir = t.Dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run()

	var found []findings.Finding
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		line = strings.TrimSpace(line)
		m := tscDiag.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lineNo, _ := strconv.Atoi(m[2])
		found = append(found, findings.Finding{
			Title:          "tsc: " + m[5],
			Classification: findings.Bug,
			Severity:       findings.P1,
			Evidence:       findings.Evidence{File: cleanPath(m[1]), Line: lineNo, Snippet: m[4] + ": " + m[5]},
			Detector:       "static.typescript.tsc",
			Anchor:         m[4] + ":" + m[5],
		})
	}
	return found, nil
}
