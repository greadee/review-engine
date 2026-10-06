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
	criteriaHeading = regexp.MustCompile(`(?i)acceptance|criteria|requirement|definition of done|checklist`)
	checkbox        = regexp.MustCompile(`^[-*]\s*\[([ xX])\]\s*(.+)$`)
	listItem        = regexp.MustCompile(`^(?:[-*]|\d+\.)\s+(.+)$`)
)

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

// Findings evaluates an issue. Unmet criteria on a closed issue are P1 (the
// closure is unsupported); on an open issue they are P3 (work in progress).
func Findings(issue vcs.Issue) []findings.Finding {
	criteria := Extract(issue.Body)
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
