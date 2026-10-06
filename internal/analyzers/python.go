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

// Python is the static analyzer for Python repositories. It uses ruff when
// available and falls back to a syntax check with the interpreter.
type Python struct{}

// NewPython returns a Python analyzer.
func NewPython() *Python { return &Python{} }

// Name implements Analyzer.
func (Python) Name() string { return "python" }

var ruffDiag = regexp.MustCompile(`^(.+?\.py):(\d+):(\d+):\s*([A-Z]+\d+)?\s*(.+)$`)

// Analyze implements Analyzer. It is a no-op when the directory is not a
// Python project or no Python tooling is available.
func (p Python) Analyze(ctx context.Context, t Target) ([]findings.Finding, error) {
	if !isPythonProject(t) {
		return nil, nil
	}
	pyFiles := filterExt(t.Files, ".py")
	if len(pyFiles) == 0 {
		return nil, nil
	}
	if ruff, err := exec.LookPath("ruff"); err == nil {
		return runRuff(ctx, t.Dir, ruff, pyFiles)
	}
	py, err := pythonInterpreter()
	if err != nil {
		return nil, nil
	}
	return compileCheck(ctx, t.Dir, py, pyFiles)
}

func isPythonProject(t Target) bool {
	for _, marker := range []string{"pyproject.toml", "setup.py", "setup.cfg", "requirements.txt"} {
		if _, err := os.Stat(filepath.Join(t.Dir, marker)); err == nil {
			return true
		}
	}
	return len(filterExt(t.Files, ".py")) > 0
}

func pythonInterpreter() (string, error) {
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no python interpreter")
}

func runRuff(ctx context.Context, dir, ruff string, files []string) ([]findings.Finding, error) {
	args := append([]string{"check", "--output-format", "concise", "--no-cache"}, files...)
	cmd := exec.CommandContext(ctx, ruff, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run() // non-zero exit simply means diagnostics were reported
	var found []findings.Finding
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		line = strings.TrimSpace(line)
		m := ruffDiag.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lineNo, _ := strconv.Atoi(m[2])
		code := strings.TrimSpace(m[4])
		msg := strings.TrimSpace(m[5])
		found = append(found, findings.Finding{
			Title:          fmt.Sprintf("ruff: %s %s", code, msg),
			Classification: findings.Bug,
			Severity:       findings.P2,
			Evidence:       findings.Evidence{File: cleanPath(m[1]), Line: lineNo, Snippet: msg},
			Detector:       "static.python.ruff",
			Anchor:         code + ":" + msg,
		})
	}
	return found, nil
}

func compileCheck(ctx context.Context, dir, py string, files []string) ([]findings.Finding, error) {
	args := append([]string{"-m", "py_compile"}, files...)
	cmd := exec.CommandContext(ctx, py, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err == nil {
		return nil, nil
	}
	return []findings.Finding{{
		Title:          "Python file failed to compile",
		Classification: findings.Bug,
		Severity:       findings.P1,
		Evidence:       findings.Evidence{Snippet: strings.TrimSpace(out.String())},
		Detector:       "static.python.compile",
		Anchor:         "compile",
	}}, nil
}

func filterExt(paths []string, ext string) []string {
	var out []string
	for _, p := range paths {
		if strings.HasSuffix(p, ext) {
			out = append(out, p)
		}
	}
	return out
}
