package report

import (
	"strings"
	"testing"
	"time"

	"github.com/greadee/review-engine/internal/findings"
)

func sample() Run {
	return Run{
		ID:             "pr@abc",
		Profile:        "pr",
		Generated:      time.Unix(0, 0).UTC(),
		BlockThreshold: findings.P1,
		Findings: []findings.Finding{
			{Title: "critical", Severity: findings.P0, Classification: findings.Security, Evidence: findings.Evidence{File: "a.go", Line: 3}, Impact: "boom"},
			{Title: "minor", Severity: findings.P3, Classification: findings.Maintainability, Evidence: findings.Evidence{File: "b.go"}},
		},
	}
}

func TestReviewMarkdown(t *testing.T) {
	md := ReviewMarkdown(sample())
	for _, want := range []string{"## Review Summary", "### Blocking", "critical", "### Non-Blocking", "minor", "Request Changes"} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q in:\n%s", want, md)
		}
	}
	if strings.Contains(md, "minor") == false {
		t.Fatal("non-blocking finding missing")
	}
}

func TestRiskAndRecommendation(t *testing.T) {
	r := sample()
	if r.Risk() != "High" || r.Recommendation() != "Request Changes" {
		t.Fatalf("got risk=%s rec=%s", r.Risk(), r.Recommendation())
	}
	clean := Run{BlockThreshold: findings.P1}
	if clean.Risk() != "Low" || clean.Recommendation() != "Approve" {
		t.Fatalf("clean run wrong: risk=%s rec=%s", clean.Risk(), clean.Recommendation())
	}
}

func TestAuditMarkdown(t *testing.T) {
	md := AuditMarkdown(sample())
	for _, want := range []string{"## Repository Audit Summary", "P0: 1", "### Result"} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q in:\n%s", want, md)
		}
	}
}
