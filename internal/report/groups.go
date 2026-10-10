package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/greadee/review-engine/internal/findings"
)

// IssueGroup is a set of findings that becomes one GitHub issue, so a planning
// agent can pick up the leftover work in tractable chunks.
type IssueGroup struct {
	Key      string
	Title    string
	Body     string
	Labels   []string
	Findings []findings.Finding
}

// GroupFindings groups findings into issues. by is "detector" (default),
// "file", or "severity".
func GroupFindings(fs []findings.Finding, by string) []IssueGroup {
	if len(fs) == 0 {
		return nil
	}
	by = strings.ToLower(strings.TrimSpace(by))
	if by == "" {
		by = "detector"
	}
	keyOf := func(f findings.Finding) string {
		switch by {
		case "file":
			if f.Evidence.File != "" {
				return f.Evidence.File
			}
			return "unknown-file"
		case "severity":
			return string(f.Severity)
		default:
			if f.Detector != "" {
				return f.Detector
			}
			return "other"
		}
	}

	groups := map[string][]findings.Finding{}
	var order []string
	for _, f := range fs {
		k := keyOf(f)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], f)
	}
	sort.Strings(order)

	var out []IssueGroup
	for _, k := range order {
		items := groups[k]
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Severity.Rank() != items[j].Severity.Rank() {
				return items[i].Severity.Rank() < items[j].Severity.Rank()
			}
			if items[i].Evidence.File != items[j].Evidence.File {
				return items[i].Evidence.File < items[j].Evidence.File
			}
			return items[i].Evidence.Line < items[j].Evidence.Line
		})
		out = append(out, IssueGroup{
			Key:      k,
			Title:    fmt.Sprintf("[revu] %s (%d finding(s))", k, len(items)),
			Body:     groupBody(by, k, items),
			Labels:   groupLabels(items),
			Findings: items,
		})
	}
	return out
}

func groupLabels(items []findings.Finding) []string {
	labels := []string{"revu"}
	sev := items[0].Severity
	same := true
	for _, f := range items {
		if f.Severity != sev {
			same = false
			break
		}
	}
	if same && sev != "" {
		labels = append(labels, string(sev))
	}
	return labels
}

func groupBody(by, key string, items []findings.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Findings from the review engine, grouped by `%s` = `%s`.\n\n", by, key)
	b.WriteString("| Severity | Location | Finding |\n|---|---|---|\n")
	for _, f := range items {
		loc := f.Evidence.File
		if f.Evidence.Line > 0 {
			loc = fmt.Sprintf("%s:%d", loc, f.Evidence.Line)
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s |\n", f.Severity, loc, f.Title)
	}
	b.WriteString("\n### Details\n\n")
	for _, f := range items {
		loc := f.Evidence.File
		if f.Evidence.Line > 0 {
			loc = fmt.Sprintf("%s:%d", loc, f.Evidence.Line)
		}
		fmt.Fprintf(&b, "- **[%s] %s** — `%s`\n", f.Severity, f.Title, loc)
		if f.Impact != "" {
			fmt.Fprintf(&b, "  - Impact: %s\n", f.Impact)
		}
		if f.Recommendation != "" {
			fmt.Fprintf(&b, "  - Recommendation: %s\n", f.Recommendation)
		}
		if f.Evidence.Snippet != "" {
			fmt.Fprintf(&b, "  - Evidence: `%s`\n", strings.TrimSpace(f.Evidence.Snippet))
		}
	}
	b.WriteString("\n_Filed automatically by review-engine._\n")
	return b.String()
}
