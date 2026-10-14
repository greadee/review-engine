// Package action tests the composite GitHub Action's contract: caller-supplied
// inputs must never be interpolated into the shell script, and hostile values
// must reach the engine as inert data.
package action

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join("..", ".."))
}

type actionFile struct {
	Inputs map[string]struct {
		Default string `yaml:"default"`
	} `yaml:"inputs"`
	Runs struct {
		Using string `yaml:"using"`
		Steps []struct {
			Name             string            `yaml:"name"`
			Shell            string            `yaml:"shell"`
			Run              string            `yaml:"run"`
			WorkingDirectory string            `yaml:"working-directory"`
			Env              map[string]string `yaml:"env"`
		} `yaml:"steps"`
	} `yaml:"runs"`
}

func parseAction(t *testing.T) actionFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var a actionFile
	if err := yaml.Unmarshal(data, &a); err != nil {
		t.Fatalf("action.yml: %v", err)
	}
	return a
}

// TestActionRunStepsAreNotInterpolated enforces the core fix for script
// injection: no `run:` script may contain a GitHub expression. Inputs are only
// allowed to appear as `env:` values, where they are data rather than script
// text.
func TestActionRunStepsAreNotInterpolated(t *testing.T) {
	a := parseAction(t)
	if a.Runs.Using != "composite" {
		t.Fatalf("expected a composite action, got %q", a.Runs.Using)
	}
	for _, step := range a.Runs.Steps {
		if strings.Contains(step.Run, "${{") {
			t.Errorf("step %q interpolates an expression into run: %q", step.Name, step.Run)
		}
	}
}

// TestActionPassesEveryInputThroughEnv ensures each declared input reaches the
// script through the environment, so the script can treat it as data.
func TestActionPassesEveryInputThroughEnv(t *testing.T) {
	a := parseAction(t)
	env := map[string]string{}
	for _, step := range a.Runs.Steps {
		for k, v := range step.Env {
			env[k] = v
		}
	}
	for name := range a.Inputs {
		expr := "${{ inputs." + name + " }}"
		found := false
		for _, v := range env {
			if v == expr {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("input %q is not passed through env", name)
		}
	}
}

// fakeRevu writes its argv (NUL-delimited) to $ARGS_FILE and exits 0.
const fakeRevu = `#!/usr/bin/env bash
printf '%s\0' "$@" > "$ARGS_FILE"
exit 0
`

func writeFakeRevu(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "revu")
	if err := os.WriteFile(path, []byte(fakeRevu), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func runScript(t *testing.T, dir string, env map[string]string) (string, error) {
	t.Helper()
	script := filepath.Join(repoRoot(t), "scripts", "run-action.sh")
	cmd := exec.Command("bash", script)
	argsFile := filepath.Join(dir, "args")
	cmd.Env = append(baseEnv(), "ARGS_FILE="+argsFile)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// baseEnv returns the ambient environment with REVIEW_* variables stripped so
// tests are not influenced by the surrounding shell.
func baseEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "REVIEW_") || strings.HasPrefix(kv, "GITHUB_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func readArgs(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatalf("fake revu was not called: %v", err)
	}
	parts := strings.Split(string(data), "\x00")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func argValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// TestHostileInputsAreInert passes a branch name full of shell metacharacters
// through the action and asserts it arrives as a single argv element without
// executing anything.
func TestHostileInputsAreInert(t *testing.T) {
	dir := t.TempDir()
	revu := writeFakeRevu(t, dir)
	marker := filepath.Join(dir, "pwned")
	hostile := "$(touch " + marker + ")\n`id`;'\"|&;$(){}[]<>*?~ "

	out, err := runScript(t, dir, map[string]string{
		"REVU_BIN":             revu,
		"REVIEW_INPUT_PHASE":   "all",
		"REVIEW_INPUT_PROFILE": "pr",
		"REVIEW_INPUT_BASE":    hostile,
		"REVIEW_INPUT_HEAD":    hostile,
		"REVIEW_INPUT_PR":      "42",
	})
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("command substitution executed: %s exists", marker)
	}
	args := readArgs(t, dir)
	if got, ok := argValue(args, "--base"); !ok || got != hostile {
		t.Fatalf("--base = %q (present=%v), want the hostile string verbatim", got, ok)
	}
	if got, ok := argValue(args, "--head"); !ok || got != hostile {
		t.Fatalf("--head = %q (present=%v), want the hostile string verbatim", got, ok)
	}
}

// TestInvalidEnumIsRejected asserts enum inputs are validated before anything
// runs, so a hostile profile cannot reach the engine.
func TestInvalidEnumIsRejected(t *testing.T) {
	dir := t.TempDir()
	revu := writeFakeRevu(t, dir)
	marker := filepath.Join(dir, "pwned")

	out, err := runScript(t, dir, map[string]string{
		"REVU_BIN":             revu,
		"REVIEW_INPUT_PROFILE": "pr; touch " + marker,
	})
	if err == nil {
		t.Fatalf("expected non-zero exit for invalid profile, got success\n%s", out)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatalf("injection executed: %s exists", marker)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "args")); statErr == nil {
		t.Fatalf("revu must not run when an input is invalid\n%s", out)
	}
	if !strings.Contains(out, "invalid profile") {
		t.Fatalf("expected an 'invalid profile' message, got:\n%s", out)
	}
}

// TestNonNumericPRIsRejected asserts numeric inputs are validated.
func TestNonNumericPRIsRejected(t *testing.T) {
	dir := t.TempDir()
	revu := writeFakeRevu(t, dir)
	out, err := runScript(t, dir, map[string]string{
		"REVU_BIN":          revu,
		"REVIEW_INPUT_PR":   "42; touch /tmp/nope",
		"REVIEW_INPUT_HEAD": "HEAD",
	})
	if err == nil {
		t.Fatalf("expected non-zero exit for non-numeric pr, got success\n%s", out)
	}
	if !strings.Contains(out, "invalid pr") {
		t.Fatalf("expected an 'invalid pr' message, got:\n%s", out)
	}
}

// TestRunFlagsFromBooleans verifies the boolean inputs still map to flags.
func TestRunFlagsFromBooleans(t *testing.T) {
	dir := t.TempDir()
	revu := writeFakeRevu(t, dir)
	if _, err := runScript(t, dir, map[string]string{
		"REVU_BIN":             revu,
		"REVIEW_INPUT_PROFILE": "pr",
		"REVIEW_INPUT_PUBLISH": "false",
		"REVIEW_INPUT_COMMENT": "false",
		"REVIEW_INPUT_BLOCK":   "false",
		"REVIEW_INPUT_PR":      "7",
	}); err != nil {
		t.Fatal(err)
	}
	args := readArgs(t, dir)
	for _, want := range []string{"run", "--profile", "pr", "--pr", "7", "--no-block"} {
		if !contains(args, want) {
			t.Errorf("expected %q in args %v", want, args)
		}
	}
	if contains(args, "--publish") || contains(args, "--comment") {
		t.Errorf("publish/comment must be off: %v", args)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
