// Package report renders review results into the review and audit templates.
package report

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/greadee/review-engine/internal/findings"
)

// Run is the rendered result of one engine run.
type Run struct {
	ID             string
	Profile        string
	Repository     string
	Ref            string
	Base           string
	Head           string
	Generated      time.Time
	Findings       []findings.Finding
	BlockThreshold findings.Severity
	// ArchiveURL links to the persisted findings source files, when available.
	ArchiveURL string
}

// Counts returns findings per severity.
func (r Run) Counts() map[findings.Severity]int {
	counts := map[findings.Severity]int{}
	for _, f := range r.Findings {
		counts[f.Severity]++
	}
	return counts
}

// Blocking returns findings at or above the block threshold.
func (r Run) Blocking() []findings.Finding {
	var out []findings.Finding
	for _, f := range r.Findings {
		if f.Severity.Rank() >= 0 && f.Severity.Rank() <= r.BlockThreshold.Rank() {
			out = append(out, f)
		}
	}
	return out
}

// NonBlocking returns findings below the block threshold.
func (r Run) NonBlocking() []findings.Finding {
	var out []findings.Finding
	for _, f := range r.Findings {
		if f.Severity.Rank() >= 0 && f.Severity.Rank() > r.BlockThreshold.Rank() {
			out = append(out, f)
		}
	}
	return out
}

// Risk classifies overall risk from the finding severities.
func (r Run) Risk() string {
	counts := r.Counts()
	switch {
	case counts[findings.P0] > 0:
		return "High"
	case counts[findings.P1] > 0:
		return "High"
	case counts[findings.P2] > 0:
		return "Medium"
	default:
		return "Low"
	}
}

// Recommendation maps risk and blocking findings to a verdict.
func (r Run) Recommendation() string {
	if len(r.Blocking()) > 0 {
		return "Request Changes"
	}
	if len(r.Findings) > 0 {
		return "Needs Investigation"
	}
	return "Approve"
}

// ReviewMarkdown renders the code-review template.
func ReviewMarkdown(r Run) string {
	var b strings.Builder
	b.WriteString("## Review Summary\n\n")
	fmt.Fprintf(&b, "_Profile: `%s` · %d finding(s) · generated %s_\n\n", r.Profile, len(r.Findings), r.Generated.UTC().Format(time.RFC3339))

	b.WriteString("### Blocking\n\n")
	writeFindings(&b, r.Blocking())
	b.WriteString("\n### Non-Blocking\n\n")
	writeFindings(&b, r.NonBlocking())

	b.WriteString("\n### Validation\n\n")
	fmt.Fprintf(&b, "- findings: %s\n", countsString(r.Counts()))
	fmt.Fprintf(&b, "- block threshold: %s\n", r.BlockThreshold)

	fmt.Fprintf(&b, "\n### Risk\n\n%s\n", r.Risk())
	fmt.Fprintf(&b, "\n### Recommendation\n\n%s\n", r.Recommendation())

	if r.ArchiveURL != "" {
		fmt.Fprintf(&b, "\n### Findings\n\nFull findings: %s\n", r.ArchiveURL)
	}
	return b.String()
}

// AuditMarkdown renders the repository-audit summary template.
func AuditMarkdown(r Run) string {
	var b strings.Builder
	b.WriteString("## Repository Audit Summary\n\n")
	b.WriteString("### Findings\n\n")
	counts := r.Counts()
	for _, sev := range []findings.Severity{findings.P0, findings.P1, findings.P2, findings.P3} {
		fmt.Fprintf(&b, "%s: %d\n", sev, counts[sev])
	}
	b.WriteString("\n### Blocking\n\n")
	writeFindings(&b, r.Blocking())
	b.WriteString("\n### Deferred\n\n")
	writeFindings(&b, r.NonBlocking())
	b.WriteString("\n### Areas Reviewed\n\n")
	fmt.Fprintf(&b, "- profile: %s\n\n", r.Profile)
	b.WriteString("### Result\n\n")
	b.WriteString(r.Recommendation())
	b.WriteString("\n")
	if r.ArchiveURL != "" {
		fmt.Fprintf(&b, "\nFull findings: %s\n", r.ArchiveURL)
	}
	return b.String()
}

// JSON renders the run as indented JSON.
func JSON(r Run) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func writeFindings(b *strings.Builder, fs []findings.Finding) {
	if len(fs) == 0 {
		b.WriteString("- None\n")
		return
	}
	for _, f := range fs {
		loc := f.Evidence.File
		if f.Evidence.Line > 0 {
			loc = fmt.Sprintf("%s:%d", loc, f.Evidence.Line)
		}
		if loc != "" {
			fmt.Fprintf(b, "- **[%s] %s** — `%s`\n", f.Severity, f.Title, loc)
		} else {
			fmt.Fprintf(b, "- **[%s] %s**\n", f.Severity, f.Title)
		}
		if f.Impact != "" {
			fmt.Fprintf(b, "  - Impact: %s\n", f.Impact)
		}
		if f.Recommendation != "" {
			fmt.Fprintf(b, "  - Recommendation: %s\n", f.Recommendation)
		}
	}
}

func countsString(counts map[findings.Severity]int) string {
	keys := make([]string, 0, len(counts))
	for _, sev := range []findings.Severity{findings.P0, findings.P1, findings.P2, findings.P3} {
		if counts[sev] > 0 {
			keys = append(keys, fmt.Sprintf("%s=%d", sev, counts[sev]))
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "none"
	}
	return strings.Join(keys, ", ")
}
