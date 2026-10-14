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

// EvidenceKind classifies a reference that can support a criterion.
type EvidenceKind int

const (
	// EvPath is a repository-relative file path (a test or source file).
	EvPath EvidenceKind = iota
	// EvRef is a pull request, issue, or commit reference (#n or a SHA).
	EvRef
	// EvApproval is an explicit "(verified)" / "(approved)" reviewer marker.
	EvApproval
)

// EvidenceRef is one independently checkable reference cited by a criterion.
type EvidenceRef struct {
	Kind  EvidenceKind
	Value string
}

// Criterion is one acceptance criterion extracted from an issue or PR body.
// Done reflects the checkbox state, which is a claim made in the text; Verified
// reflects independent evidence and is what a completion gate must rely on.
type Criterion struct {
	Text     string
	Done     bool
	Evidence []EvidenceRef
}

// Verified reports whether the criterion carries independently checkable
// evidence. A repository path must resolve through read; a PR/issue/commit
// reference or an explicit approval marker counts without a resolver. A
// checkbox alone is never sufficient.
func (c Criterion) Verified(read DocReader) bool {
	for _, e := range c.Evidence {
		switch e.Kind {
		case EvRef, EvApproval:
			return true
		case EvPath:
			if read != nil {
				if _, ok := read(e.Value); ok {
					return true
				}
			}
		}
	}
	return false
}

var (
	criteriaHeading = regexp.MustCompile(`(?i)\b(acceptance criteria|acceptance|criteria|definition of done|checklist)\b`)
	checkbox        = regexp.MustCompile(`^[-*]\s*\[([ xX])\]\s*(.+)$`)
	listItem        = regexp.MustCompile(`^(?:[-*]|\d+\.)\s+(.+)$`)
	docRef          = regexp.MustCompile("`([^`\\n]+\\.md)`")
	markdownDocLink = regexp.MustCompile(`\]\(([^)\s]+\.md)\)`)

	evidencePath    = regexp.MustCompile("`([^`\\n]+\\.[A-Za-z0-9]+)`")
	evidenceLink    = regexp.MustCompile(`\]\(([^)\s]+\.[A-Za-z0-9]+)\)`)
	evidenceRef     = regexp.MustCompile(`(?:#\d+|\b[0-9a-f]{7,40}\b)`)
	evidenceApprove = regexp.MustCompile(`(?i)\((verified|approved)\)`)
)

// parseEvidence extracts independently checkable references from a criterion's
// text: linked files, PR/issue/commit references, and explicit approval markers.
func parseEvidence(text string) []EvidenceRef {
	var out []EvidenceRef
	seen := map[string]bool{}
	add := func(kind EvidenceKind, value string) {
		key := fmt.Sprintf("%d\x00%s", kind, value)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, EvidenceRef{Kind: kind, Value: value})
	}
	if evidenceApprove.MatchString(text) {
		add(EvApproval, "approved")
	}
	for _, m := range evidencePath.FindAllStringSubmatch(text, -1) {
		add(EvPath, strings.TrimSpace(m[1]))
	}
	for _, m := range evidenceLink.FindAllStringSubmatch(text, -1) {
		add(EvPath, strings.TrimSpace(m[1]))
	}
	for _, m := range evidenceRef.FindAllString(text, -1) {
		add(EvRef, m)
	}
	return out
}

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
		out = append(out, Criterion{Text: text, Done: done, Evidence: parseEvidence(text)})
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

// Verdict summarizes an issue's acceptance-criteria status. Verified counts
// criteria backed by independent evidence; Checked and Unverified describe the
// checkbox state, which is reported but never sufficient on its own.
type Verdict struct {
	Criteria    int
	Checked     int
	Verified    int
	Unverified  int
	Unmet       int
	HasCriteria bool
	// Approved records an explicit reviewer approval (a configured label).
	Approved bool
}

// GoodToGo reports whether the issue may be closed: it must have criteria and
// every criterion must be independently verified, or a reviewer must have
// explicitly approved it. Checklist state alone is never sufficient.
func (v Verdict) GoodToGo() bool {
	return v.HasCriteria && (v.Unmet == 0 || v.Approved)
}

// Options tunes evaluation.
type Options struct {
	// Approved marks the issue as explicitly approved by a reviewer.
	Approved bool
}

// EvaluateWith computes the verdict from the issue body and any referenced
// docs the reader can resolve (a nil reader uses the body only).
func EvaluateWith(issue vcs.Issue, read DocReader) Verdict {
	return EvaluateWithOptions(issue, read, Options{})
}

// EvaluateWithOptions computes the verdict with evaluation options.
func EvaluateWithOptions(issue vcs.Issue, read DocReader, opts Options) Verdict {
	criteria := CollectCriteria(issue.Body, read)
	v := Verdict{Criteria: len(criteria), HasCriteria: len(criteria) > 0, Approved: opts.Approved}
	for _, c := range criteria {
		if c.Done {
			v.Checked++
		}
		if c.Verified(read) {
			v.Verified++
		} else {
			v.Unmet++
			if c.Done {
				v.Unverified++
			}
		}
	}
	return v
}

// Unmet returns the criteria that are not independently verified.
func Unmet(issue vcs.Issue, read DocReader) []Criterion {
	var out []Criterion
	for _, c := range CollectCriteria(issue.Body, read) {
		if !c.Verified(read) {
			out = append(out, c)
		}
	}
	return out
}

var areaToken = regexp.MustCompile("`([a-zA-Z][\\w./-]*/[\\w./-]+)`")

// Areas returns the module/directory prefixes an issue relates to, taken from
// path-like tokens in its body and referenced docs.
func Areas(issue vcs.Issue, read DocReader) []string {
	texts := []string{issue.Body}
	if read != nil {
		for _, p := range ReferencedDocs(issue.Body) {
			// Only the issue's own doc defines its areas; sprint plans and other
			// docs reference too many modules.
			if !strings.HasPrefix(p, "docs/issues/") {
				continue
			}
			if content, ok := read(p); ok {
				texts = append(texts, content)
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, text := range texts {
		for _, m := range areaToken.FindAllStringSubmatch(text, -1) {
			area := normalizeArea(m[1])
			if !strings.Contains(area, "/") || strings.HasPrefix(area, "docs/") || seen[area] {
				continue
			}
			seen[area] = true
			out = append(out, area)
		}
	}
	return out
}

// normalizeArea drops a trailing symbol (e.g. routing.Decide -> routing) or
// file extension from the final path segment.
func normalizeArea(tok string) string {
	tok = strings.TrimPrefix(strings.TrimSpace(tok), "./")
	parts := strings.Split(tok, "/")
	last := parts[len(parts)-1]
	if i := strings.Index(last, "."); i >= 0 {
		last = last[:i]
	}
	parts[len(parts)-1] = last
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "/")
}

// RelatedFindings returns the findings that fall inside the issue's areas.
func RelatedFindings(issue vcs.Issue, read DocReader, all []findings.Finding) []findings.Finding {
	areas := Areas(issue, read)
	if len(areas) == 0 {
		return nil
	}
	var out []findings.Finding
	for _, f := range all {
		if fileInAreas(f.Evidence.File, areas) {
			out = append(out, f)
		}
	}
	return out
}

func fileInAreas(file string, areas []string) bool {
	file = strings.TrimPrefix(strings.TrimSpace(file), "./")
	for _, a := range areas {
		if file == a || strings.HasPrefix(file, a+"/") {
			return true
		}
	}
	return false
}

// FindingsWith evaluates an issue's acceptance criteria. A criterion is only
// treated as satisfied when it carries independent evidence; a checkbox alone
// is reported as unverified. Unverified criteria on a closed issue are P2 (the
// closure is unsupported by evidence) and on an open issue are P3; criteria
// that are not even checked are P1/P3 accordingly. A nil reader restricts
// evaluation to the issue body.
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
		if c.Verified(read) {
			continue
		}
		sev := findings.P3
		title := "Acceptance criterion outstanding"
		impact := "The issue is still open; this criterion has no evidence of completion."
		if c.Done {
			title = "Acceptance criterion is checked but not independently verified"
			impact = "A checkbox claims completion, but no linked test, change, or approval supports it."
		}
		if closed {
			if c.Done {
				sev = findings.P2
				title = "Acceptance criterion is checked but unverified on a closed issue"
				impact = "The issue was closed on a checklist claim without independent evidence."
			} else {
				sev = findings.P1
				title = "Acceptance criterion not met on a closed issue"
				impact = "The issue was closed without satisfying this criterion."
			}
		}
		out = append(out, findings.Finding{
			Title:          title,
			Classification: findings.Bug,
			Severity:       sev,
			Evidence:       findings.Evidence{File: fmt.Sprintf("issue#%d", issue.Number), Snippet: c.Text},
			Impact:         impact,
			Recommendation: "Link a test, reviewed change, PR/commit, or add an explicit (verified) approval; updating the criterion text alone is not evidence.",
			Detector:       "issue.criteria",
			Anchor:         "criterion:" + strings.ToLower(c.Text),
		})
	}
	return out
}
