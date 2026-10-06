// Package findings defines the durable finding record, its severity and
// classification vocabulary, deterministic fingerprinting, and the lifecycle
// that lets findings be tracked across runs.
package findings

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Severity is the review severity vocabulary. P0/P1 normally block.
type Severity string

const (
	P0 Severity = "P0"
	P1 Severity = "P1"
	P2 Severity = "P2"
	P3 Severity = "P3"
)

var severityRank = map[Severity]int{P0: 0, P1: 1, P2: 2, P3: 3}

// Valid reports whether s is a known severity.
func (s Severity) Valid() bool { _, ok := severityRank[s]; return ok }

// Rank returns the ordering rank (P0 lowest) for sorting. Unknown is treated
// as most severe so it is never silently hidden.
func (s Severity) Rank() int {
	if r, ok := severityRank[s]; ok {
		return r
	}
	return -1
}

// Classification categorizes a finding.
type Classification string

const (
	Bug             Classification = "Bug"
	TechnicalDebt   Classification = "Technical Debt"
	Refactor        Classification = "Refactor"
	Security        Classification = "Security"
	Test            Classification = "Test"
	Documentation   Classification = "Documentation"
	Architecture    Classification = "Architecture"
	Reliability     Classification = "Reliability"
	Performance     Classification = "Performance"
	Maintainability Classification = "Maintainability"
	Data            Classification = "Data"
)

var classifications = map[Classification]bool{
	Bug: true, TechnicalDebt: true, Refactor: true, Security: true,
	Test: true, Documentation: true, Architecture: true, Reliability: true,
	Performance: true, Maintainability: true, Data: true,
}

// Valid reports whether c is a known classification.
func (c Classification) Valid() bool { return classifications[c] }

// Status is the lifecycle state of a tracked finding.
type Status string

const (
	StatusNew       Status = "new"
	StatusOngoing   Status = "ongoing"
	StatusResolved  Status = "resolved"
	StatusRegressed Status = "regressed"
)

// Evidence locates a finding in the code.
type Evidence struct {
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// Finding is a single review result. It carries enough context to survive the
// session and be actioned later.
type Finding struct {
	ID             string         `json:"id"`
	Title          string         `json:"title"`
	Classification Classification `json:"classification"`
	Severity       Severity       `json:"severity"`
	Evidence       Evidence       `json:"evidence"`
	Impact         string         `json:"impact,omitempty"`
	Recommendation string         `json:"recommendation,omitempty"`
	Scope          string         `json:"scope,omitempty"`
	SuggestedPhase string         `json:"suggestedPhase,omitempty"`
	Profile        string         `json:"profile,omitempty"`
	Detector       string         `json:"detector,omitempty"`
	// Anchor is a short, stable code anchor supplied by the detector. It is
	// used for fingerprinting so that LLM prose changes do not churn the
	// finding's identity.
	Anchor    string `json:"anchor,omitempty"`
	Status    Status `json:"status"`
	FirstSeen string `json:"firstSeen,omitempty"`
	LastSeen  string `json:"lastSeen,omitempty"`
	RunID     string `json:"runId,omitempty"`
}

// Validate reports whether the finding is well formed. It does not require an
// ID; Set.Add computes a fingerprint when one is absent.
func (f Finding) Validate() error {
	if strings.TrimSpace(f.Title) == "" {
		return fmt.Errorf("finding: title is required")
	}
	if !f.Severity.Valid() {
		return fmt.Errorf("finding: invalid severity %q", f.Severity)
	}
	if !f.Classification.Valid() {
		return fmt.Errorf("finding: invalid classification %q", f.Classification)
	}
	return nil
}

// Fingerprint derives a stable identity for a finding from its location, the
// detector that produced it, and a code anchor. It deliberately excludes
// free-form prose so that rewording does not change identity.
func Fingerprint(file, detector, anchor string) string {
	normalizedFile := normalizePath(file)
	normalizedDetector := strings.ToLower(strings.TrimSpace(detector))
	normalizedAnchor := strings.ToLower(strings.Join(strings.Fields(anchor), " "))
	sum := sha256.Sum256([]byte(normalizedFile + "\x00" + normalizedDetector + "\x00" + normalizedAnchor))
	return hex.EncodeToString(sum[:16])
}

func normalizePath(p string) string {
	p = strings.TrimSpace(filepath.ToSlash(p))
	p = strings.TrimPrefix(p, "./")
	return strings.ToLower(p)
}

// EnsureID fills ID with a fingerprint when it is empty.
func (f *Finding) EnsureID() {
	if f.ID == "" {
		f.ID = Fingerprint(f.Evidence.File, f.Detector, f.Anchor)
	}
}

// Set is an ordered, de-duplicated collection of findings keyed by ID.
type Set struct {
	items []Finding
	index map[string]int
}

// NewSet returns an empty set.
func NewSet() *Set { return &Set{index: map[string]int{}} }

// Add inserts or merges a finding. Later duplicates enrich an existing record
// with any non-empty fields the existing record lacked.
func (s *Set) Add(f Finding) {
	f.EnsureID()
	if i, ok := s.index[f.ID]; ok {
		merge(&s.items[i], f)
		return
	}
	s.index[f.ID] = len(s.items)
	s.items = append(s.items, f)
}

func merge(dst *Finding, src Finding) {
	if dst.Title == "" {
		dst.Title = src.Title
	}
	if dst.Classification == "" {
		dst.Classification = src.Classification
	}
	if dst.Severity == "" || (src.Severity.Valid() && src.Severity.Rank() < dst.Severity.Rank()) {
		dst.Severity = src.Severity
	}
	if dst.Evidence.File == "" {
		dst.Evidence = src.Evidence
	}
	if dst.Impact == "" {
		dst.Impact = src.Impact
	}
	if dst.Recommendation == "" {
		dst.Recommendation = src.Recommendation
	}
	if dst.Scope == "" {
		dst.Scope = src.Scope
	}
	if dst.SuggestedPhase == "" {
		dst.SuggestedPhase = src.SuggestedPhase
	}
	if dst.Profile == "" {
		dst.Profile = src.Profile
	}
	if dst.Detector == "" {
		dst.Detector = src.Detector
	}
	if dst.Anchor == "" {
		dst.Anchor = src.Anchor
	}
}

// Items returns a severity-sorted copy (P0 first, then file/line/title).
func (s *Set) Items() []Finding {
	out := make([]Finding, len(s.items))
	copy(out, s.items)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity.Rank() != out[j].Severity.Rank() {
			return out[i].Severity.Rank() < out[j].Severity.Rank()
		}
		if out[i].Evidence.File != out[j].Evidence.File {
			return out[i].Evidence.File < out[j].Evidence.File
		}
		if out[i].Evidence.Line != out[j].Evidence.Line {
			return out[i].Evidence.Line < out[j].Evidence.Line
		}
		return out[i].Title < out[j].Title
	})
	return out
}

// Len returns the number of findings.
func (s *Set) Len() int { return len(s.items) }

// Counts returns the number of findings per severity.
func (s *Set) Counts() map[Severity]int {
	counts := map[Severity]int{}
	for _, f := range s.items {
		counts[f.Severity]++
	}
	return counts
}

// Blocking returns findings at or above the given severity threshold.
func (s *Set) Blocking(threshold Severity) []Finding {
	var out []Finding
	for _, f := range s.Items() {
		if f.Severity.Rank() >= 0 && f.Severity.Rank() <= threshold.Rank() {
			out = append(out, f)
		}
	}
	return out
}
