// Package profiles defines the built-in review profiles and their defaults.
package profiles

// Built-in profile names.
const (
	PR     = "pr"
	Issue  = "issue"
	Sprint = "sprint"
	Audit  = "audit"
	Impact = "impact"
)

var order = []string{PR, Issue, Sprint, Audit, Impact}

var rubrics = map[string][]string{
	PR:     {"correctness", "scope", "architecture", "maintainability", "test quality", "error handling", "security", "backwards compatibility", "missing implementation"},
	Issue:  {"acceptance criteria coverage", "unimplemented requirements", "test coverage", "documentation", "scope creep"},
	Sprint: {"coherence", "incomplete work", "regressions", "consistency", "missing implementation"},
	Audit:  {"correctness", "architecture", "tests", "security", "data", "reliability", "performance", "maintainability", "documentation", "repository hygiene"},
	Impact: {"regressions", "orphaned code", "dependency direction", "broken callers", "missing migration"},
}

// All returns the profile names in order.
func All() []string {
	out := make([]string, len(order))
	copy(out, order)
	return out
}

// Valid reports whether name is a known profile.
func Valid(name string) bool {
	_, ok := rubrics[name]
	return ok
}

// Rubric returns the default rubric for a profile, or the PR rubric when
// unknown.
func Rubric(name string) []string {
	if r, ok := rubrics[name]; ok {
		return append([]string(nil), r...)
	}
	return append([]string(nil), rubrics[PR]...)
}
