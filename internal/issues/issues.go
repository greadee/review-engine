// Package issues reviews an issue's delivered work against its stated
// acceptance criteria. It is the engine behind the issue profile and the
// issue-close gate.
package issues

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/vcs"
)

// Criterion is one acceptance criterion extracted from an issue or PR body.
type Criterion struct {
	Text string
	Done bool
}

var (
	criteriaHeading = regexp.MustCompile(`(?i)\b(acceptance criteria|acceptance|criteria|definition of done|checklist)\b`)
	checkbox        = regexp.MustCompile(`^[-*]\s*\[([ xX])\]\s*(.+)$`)
	listItem        = regexp.MustCompile(`^(?:[-*]|\d+\.)\s+(.+)$`)
	docRef          = regexp.MustCompile("`([^`\\n]+\\.md)`")
	markdownDocLink = regexp.MustCompile(`\]\(([^)\s]+\.md)\)`)
)

// DocReader returns the contents of a repository-relative Markdown path. It is
// used to pull acceptance criteria from docs an issue body references.
type DocReader func(path string) (string, bool)

// ReferencedDocs returns the repository-relative Markdown paths an issue body
// points at (backticked paths and Markdown links).
func ReferencedDocs(body string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		p = strings.TrimPrefix(p, "./")
		if p == "" || strings.Contains(p, "://") || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, m := range docRef.FindAllStringSubmatch(body, -1) {
		add(m[1])
	}
	for _, m := range markdownDocLink.FindAllStringSubmatch(body, -1) {
		add(m[1])
	}
	return out
}

// CollectCriteria merges criteria found in the issue body with criteria found
// in the docs it references, so an issue whose checklist lives in a doc is
// evaluated correctly.
func CollectCriteria(body string, read DocReader) []Criterion {
	merged := Extract(body)
	seen := map[string]bool{}
	for _, c := range merged {
		seen[strings.ToLower(c.Text)] = true
	}
	if read != nil {
		for _, p := range ReferencedDocs(body) {
			content, ok := read(p)
			if !ok {
				continue
			}
			for _, c := range Extract(content) {
				key := strings.ToLower(c.Text)
				if seen[key] {
					continue
				}
				seen[key] = true
				merged = append(merged, c)
			}
		}
	}
	return merged
}

// Extract pulls acceptance criteria from a Markdown body: every checkbox,
// plus list items that appear under an acceptance/criteria heading.
func Extract(body string) []Criterion {
	var out []Criterion
	seen := map[string]bool{}
	add := func(text string, done bool) {
		text = strings.TrimSpace(text)
		key := strings.ToLower(text)
		if text == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Criterion{Text: text, Done: done})
	}

	inSection := false
	sectionLevel := 0
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			title := strings.TrimSpace(trimmed[level:])
			if criteriaHeading.MatchString(title) {
				inSection = true
				sectionLevel = level
			} else if inSection && level <= sectionLevel {
				inSection = false
			}
			continue
		}
		if m := checkbox.FindStringSubmatch(trimmed); m != nil {
			add(m[2], strings.EqualFold(m[1], "x"))
			continue
		}
		if inSection {
			if m := listItem.FindStringSubmatch(trimmed); m != nil {
				add(m[1], false)
			}
		}
	}
	return out
}

// Verdict summarizes an issue's acceptance-criteria status.
type Verdict struct {
	Criteria    int
	Met         int
	Unmet       int
	HasCriteria bool
}

// GoodToGo reports whether the issue has criteria and all are satisfied, making
// it safe to close.
func (v Verdict) GoodToGo() bool { return v.HasCriteria && v.Unmet == 0 }

// EvaluateWith computes the verdict from the issue body and any referenced
// docs the reader can resolve (a nil reader uses the body only).
func EvaluateWith(issue vcs.Issue, read DocReader) Verdict {
	criteria := CollectCriteria(issue.Body, read)
	v := Verdict{Criteria: len(criteria), HasCriteria: len(criteria) > 0}
	for _, c := range criteria {
		if c.Done {
			v.Met++
		} else {
			v.Unmet++
		}
	}
	return v
}

// Unmet returns the outstanding criteria for an issue.
func Unmet(issue vcs.Issue, read DocReader) []Criterion {
	var out []Criterion
	for _, c := range CollectCriteria(issue.Body, read) {
		if !c.Done {
			out = append(out, c)
		}
	}
	return out
}

// FindingsWith evaluates an issue. Unmet criteria on a closed issue are P1 (the
// closure is unsupported); on an open issue they are P3 (work in progress). A
// nil reader restricts evaluation to the issue body.
func FindingsWith(issue vcs.Issue, read DocReader) []findings.Finding {
	criteria := CollectCriteria(issue.Body, read)
	var out []findings.Finding
	if len(criteria) == 0 {
		out = append(out, findings.Finding{
			Title:          "Issue has no acceptance criteria",
			Classification: findings.Documentation,
			Severity:       findings.P3,
			Evidence:       findings.Evidence{File: fmt.Sprintf("issue#%d", issue.Number)},
			Impact:         "Delivered work cannot be verified against stated criteria.",
			Recommendation: "Add an acceptance-criteria checklist to the issue.",
			Detector:       "issue.criteria",
			Anchor:         "no-criteria",
		})
		return out
	}
	closed := strings.EqualFold(issue.State, "closed")
	for _, c := range criteria {
		if c.Done {
			continue
		}
		sev := findings.P3
		title := "Acceptance criterion outstanding"
		impact := "The issue is still open; this criterion is not yet satisfied."
		if closed {
			sev = findings.P1
			title = "Acceptance criterion not met on a closed issue"
			impact = "The issue was closed without satisfying this criterion."
		}
		out = append(out, findings.Finding{
			Title:          title,
			Classification: findings.Bug,
			Severity:       sev,
			Evidence:       findings.Evidence{File: fmt.Sprintf("issue#%d", issue.Number), Snippet: c.Text},
			Impact:         impact,
			Recommendation: "Complete the work, or update the criterion if it no longer applies.",
			Detector:       "issue.criteria",
			Anchor:         "criterion:" + strings.ToLower(c.Text),
		})
	}
	return out
}
