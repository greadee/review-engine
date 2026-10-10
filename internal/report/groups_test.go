package report

import (
	"strings"
	"testing"

	"github.com/greadee/review-engine/internal/findings"
)

func sampleFindings() []findings.Finding {
	return []findings.Finding{
		{Title: "a", Severity: findings.P2, Classification: findings.Bug, Detector: "detector.orphan", Evidence: findings.Evidence{File: "x.go", Line: 1}},
		{Title: "b", Severity: findings.P3, Classification: findings.Bug, Detector: "detector.orphan", Evidence: findings.Evidence{File: "y.go"}},
		{Title: "c", Severity: findings.P1, Classification: findings.Security, Detector: "detector.fail-open", Evidence: findings.Evidence{File: "z.go"}},
	}
}

func TestGroupFindingsByDetector(t *testing.T) {
	g := GroupFindings(sampleFindings(), "detector")
	if len(g) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(g))
	}
	if g[0].Key != "detector.fail-open" || len(g[0].Findings) != 1 {
		t.Fatalf("unexpected first group: %+v", g[0].Key)
	}
	if g[1].Key != "detector.orphan" || len(g[1].Findings) != 2 {
		t.Fatalf("unexpected second group: %+v", g[1])
	}
	if !strings.Contains(g[1].Body, "x.go") || !strings.Contains(g[1].Title, "detector.orphan") {
		t.Fatalf("body/title missing content:\n%s", g[1].Body)
	}
	// Severity ordering within a group: P2 before P3.
	if g[1].Findings[0].Severity != findings.P2 {
		t.Fatalf("expected severity-sorted group: %+v", g[1].Findings)
	}
}

func TestGroupFindingsBySeverityAndEmpty(t *testing.T) {
	if g := GroupFindings(sampleFindings(), "severity"); len(g) != 3 {
		t.Fatalf("expected 3 severity groups, got %d", len(g))
	}
	if GroupFindings(nil, "detector") != nil {
		t.Fatal("empty input should produce no groups")
	}
}
