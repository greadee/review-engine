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

func TestDelta(t *testing.T) {
	r := Run{Findings: []findings.Finding{
		{Status: findings.StatusNew},
		{Status: findings.StatusOngoing},
		{Status: findings.StatusRegressed},
		{Status: findings.StatusOngoing},
	}, Resolved: []findings.Finding{{Status: findings.StatusResolved}}}
	d := r.Delta()
	if d.New != 1 || d.Ongoing != 2 || d.Regressed != 1 || d.Resolved != 1 {
		t.Fatalf("unexpected delta %+v", d)
	}
	if !strings.Contains(ReviewMarkdown(r), "delta: new=1 ongoing=2 regressed=1 resolved=1") {
		t.Fatal("review markdown missing delta")
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

func TestUnverifiedFindingsDoNotBlock(t *testing.T) {
	r := Run{BlockThreshold: findings.P1, Findings: []findings.Finding{
		{Title: "forged", Severity: findings.P0, Classification: findings.Security, Evidence: findings.Evidence{File: "a.go"}, Unverified: true},
	}}
	if len(r.Blocking()) != 0 {
		t.Fatalf("an unverified finding must not block: %+v", r.Blocking())
	}
	if len(r.NonBlocking()) != 1 {
		t.Fatalf("an unverified finding should still be reported, got %d", len(r.NonBlocking()))
	}
	if r.Recommendation() == "Approve" {
		t.Fatal("must not approve while any finding exists")
	}
	if !strings.Contains(ReviewMarkdown(r), "evidence unverified") {
		t.Fatal("report should label unverified evidence")
	}
}

func TestSanitizeNeutralizesUntrustedText(t *testing.T) {
	r := Run{BlockThreshold: findings.P1, Findings: []findings.Finding{
		{Title: "<script>alert(1)</script> @team", Severity: findings.P2, Classification: findings.Bug, Evidence: findings.Evidence{File: "a.go", Line: 1}, Impact: "@here fix <b>"},
	}}
	md := ReviewMarkdown(r)
	if strings.Contains(md, "<script>") {
		t.Fatalf("raw HTML leaked:\n%s", md)
	}
	if strings.Contains(md, "@team") || strings.Contains(md, "@here") {
		t.Fatalf("mentions not defused:\n%s", md)
	}
	if !strings.Contains(md, "&lt;script&gt;") {
		t.Fatalf("expected escaped HTML:\n%s", md)
	}
}
