package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/greadee/review-engine/internal/findings"
)

// IssueGroup is a set of findings, used as one section (or one issue).
type IssueGroup struct {
	Key      string
	Title    string
	Body     string
	Labels   []string
	Findings []findings.Finding
}

// GroupFindings groups findings by "detector" (default), "file", or "severity".
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
		items := sortFindings(groups[k])
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

// ConsolidatedBody renders every finding into one Markdown issue body, sectioned
// by the requested grouping.
func ConsolidatedBody(r Run, by string) string {
	var b strings.Builder
	counts := r.Counts()
	fmt.Fprintf(&b, "Automated review findings for profile `%s`.\n\n", firstNonEmpty(r.Profile, "review"))
	fmt.Fprintf(&b, "**%d finding(s)** — P0=%d P1=%d P2=%d P3=%d.\n\n",
		len(r.Findings), counts[findings.P0], counts[findings.P1], counts[findings.P2], counts[findings.P3])
	if r.ID != "" {
		fmt.Fprintf(&b, "Run: `%s`.\n\n", r.ID)
	}
	if r.ArchiveURL != "" {
		fmt.Fprintf(&b, "Archived findings: %s\n\n", r.ArchiveURL)
	}
	b.WriteString("---\n\n")
	for _, g := range GroupFindings(r.Findings, by) {
		fmt.Fprintf(&b, "## %s (%d)\n\n", g.Key, len(g.Findings))
		writeTable(&b, g.Findings)
		b.WriteString("\n")
		writeDetails(&b, g.Findings)
		b.WriteString("\n")
	}
	b.WriteString("_Filed automatically by review-engine._\n")
	return b.String()
}

func sortFindings(items []findings.Finding) []findings.Finding {
	out := make([]findings.Finding, len(items))
	copy(out, items)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity.Rank() != out[j].Severity.Rank() {
			return out[i].Severity.Rank() < out[j].Severity.Rank()
		}
		if out[i].Evidence.File != out[j].Evidence.File {
			return out[i].Evidence.File < out[j].Evidence.File
		}
		return out[i].Evidence.Line < out[j].Evidence.Line
	})
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
	fmt.Fprintf(&b, "Findings grouped by `%s` = `%s`.\n\n", by, key)
	writeTable(&b, items)
	b.WriteString("\n")
	writeDetails(&b, items)
	b.WriteString("\n_Filed automatically by review-engine._\n")
	return b.String()
}

func writeTable(b *strings.Builder, items []findings.Finding) {
	b.WriteString("| Severity | Location | Finding |\n|---|---|---|\n")
	for _, f := range items {
		fmt.Fprintf(b, "| %s | `%s` | %s |\n", f.Severity, loc(f), f.Title)
	}
}

func writeDetails(b *strings.Builder, items []findings.Finding) {
	b.WriteString("### Details\n\n")
	for _, f := range items {
		fmt.Fprintf(b, "- **[%s] %s** — `%s`\n", f.Severity, f.Title, loc(f))
		if f.Impact != "" {
			fmt.Fprintf(b, "  - Impact: %s\n", f.Impact)
		}
		if f.Recommendation != "" {
			fmt.Fprintf(b, "  - Recommendation: %s\n", f.Recommendation)
		}
		if f.Evidence.Snippet != "" {
			fmt.Fprintf(b, "  - Evidence: `%s`\n", strings.TrimSpace(f.Evidence.Snippet))
		}
	}
}

func loc(f findings.Finding) string {
	if f.Evidence.Line > 0 {
		return fmt.Sprintf("%s:%d", f.Evidence.File, f.Evidence.Line)
	}
	return f.Evidence.File
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
