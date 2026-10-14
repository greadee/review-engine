package issues

import (
	"testing"

	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/vcs"
)

const body = `# Feature

Some context.

## Acceptance criteria

- [x] It compiles
- [ ] It is wired into production
- [ ] Tests cover the error path

## Notes

- [x] not a criterion? it is a checkbox
`

func TestExtract(t *testing.T) {
	got := Extract(body)
	byText := map[string]bool{}
	for _, c := range got {
		byText[c.Text] = c.Done
	}
	if !byText["It compiles"] {
		t.Fatal("checked criterion should be done")
	}
	if _, ok := byText["It is wired into production"]; !ok {
		t.Fatalf("unchecked criterion missing: %+v", got)
	}
	if !byText["not a criterion? it is a checkbox"] {
		t.Fatal("checkbox outside section should still count")
	}
}

func TestFindingsClosedUnmetIsP1(t *testing.T) {
	got := FindingsWith(vcs.Issue{Number: 7, State: "closed", Body: body}, nil)
	var p1, p3 int
	for _, f := range got {
		switch f.Severity {
		case findings.P1:
			p1++
		case findings.P3:
			p3++
		}
	}
	if p1 != 2 {
		t.Fatalf("expected 2 P1 unmet criteria on closed issue, got %d in %+v", p1, got)
	}
}

func TestFindingsOpenUnmetIsP3(t *testing.T) {
	got := FindingsWith(vcs.Issue{Number: 7, State: "open", Body: body}, nil)
	for _, f := range got {
		if f.Severity == findings.P1 {
			t.Fatalf("open issue should not produce P1: %+v", f)
		}
	}
}

func TestRelatedFindingsAndAreas(t *testing.T) {
	body := "Docs: `docs/issues/imp.md`\n"
	read := func(p string) (string, bool) {
		if p == "docs/issues/imp.md" {
			return "# ISS-IMP — runtime: sandbox\n\n`runtime/sandbox` and `kernel/allocator/routing.Decide`.\n", true
		}
		return "", false
	}
	areas := Areas(vcs.Issue{Body: body}, read)
	want := map[string]bool{"runtime/sandbox": true, "kernel/allocator/routing": true}
	if len(areas) != len(want) {
		t.Fatalf("unexpected areas: %v", areas)
	}
	for _, a := range areas {
		if !want[a] {
			t.Fatalf("unexpected area %q (have %v)", a, areas)
		}
	}
	all := []findings.Finding{
		{Evidence: findings.Evidence{File: "runtime/sandbox/sandbox.go"}},
		{Evidence: findings.Evidence{File: "kernel/allocator/routing/routing.go"}},
		{Evidence: findings.Evidence{File: "memory/retrieval/retrieve.go"}},
	}
	rel := RelatedFindings(vcs.Issue{Body: body}, read, all)
	if len(rel) != 2 {
		t.Fatalf("expected 2 related findings, got %d: %+v", len(rel), rel)
	}
}

func TestCollectCriteriaFromDocs(t *testing.T) {
	body := "Docs: `docs/issues/imp.md`\n\nSprint stuff.\n"
	read := func(p string) (string, bool) {
		if p == "docs/issues/imp.md" {
			return "## Acceptance Criteria\n\n- [x] first\n- [ ] second\n", true
		}
		return "", false
	}
	criteria := CollectCriteria(body, read)
	if len(criteria) != 2 {
		t.Fatalf("expected 2 criteria from the doc, got %+v", criteria)
	}
	v := EvaluateWith(vcs.Issue{Number: 1, State: "open", Body: body}, read)
	if !v.HasCriteria || v.Verified != 0 || v.Unmet != 2 || v.GoodToGo() {
		t.Fatalf("unexpected verdict: %+v", v)
	}
	// Without a reader, only the body is consulted (none here).
	if got := EvaluateWith(vcs.Issue{Body: body}, nil); got.HasCriteria {
		t.Fatalf("expected no criteria without a reader, got %+v", got)
	}
}

func TestEvaluate(t *testing.T) {
	v := EvaluateWith(vcs.Issue{State: "open", Body: body}, nil)
	if !v.HasCriteria || v.Criteria != 4 || v.Checked != 2 || v.Verified != 0 || v.Unmet != 4 {
		t.Fatalf("unexpected verdict: %+v", v)
	}
	if v.GoodToGo() {
		t.Fatal("issue with unverified criteria must not be good-to-go")
	}

	// Only independently evidenced criteria make an issue good-to-go.
	gtg := EvaluateWith(vcs.Issue{State: "open", Body: "## Acceptance criteria\n\n- [x] done ref #12\n- [x] also done ref #13\n"}, nil)
	if !gtg.GoodToGo() || gtg.Unmet != 0 || gtg.Verified != 2 {
		t.Fatalf("expected good-to-go with evidence: %+v", gtg)
	}

	none := EvaluateWith(vcs.Issue{Body: "no list here"}, nil)
	if none.HasCriteria || none.GoodToGo() {
		t.Fatalf("no criteria must not be good-to-go: %+v", none)
	}
}

// TestCheckedButUnverifiedNotGoodToGo covers the "checked-but-undelivered"
// case: ticking a box is a claim, not evidence.
func TestCheckedButUnverifiedNotGoodToGo(t *testing.T) {
	issue := vcs.Issue{Number: 5, State: "closed", Body: "## Acceptance criteria\n\n- [x] shipped the feature\n"}
	v := EvaluateWith(issue, nil)
	if v.GoodToGo() {
		t.Fatalf("checked criterion must not be good-to-go without evidence: %+v", v)
	}
	if v.Checked != 1 || v.Verified != 0 || v.Unverified != 1 || v.Unmet != 1 {
		t.Fatalf("unexpected verdict: %+v", v)
	}
	got := FindingsWith(issue, nil)
	if len(got) != 1 || got[0].Severity != findings.P2 {
		t.Fatalf("expected one P2 unverified-criterion finding, got %+v", got)
	}
}

// TestDeliveredButUncheckedIsVerified covers the "delivered-but-unchecked"
// case: independent evidence satisfies a criterion even if the box is unticked.
func TestDeliveredButUncheckedIsVerified(t *testing.T) {
	read := func(p string) (string, bool) {
		if p == "internal/auth/login_test.go" {
			return "package auth", true
		}
		return "", false
	}
	body := "## Acceptance criteria\n\n- [ ] login rejects bad passwords — `internal/auth/login_test.go`\n"
	v := EvaluateWithOptions(vcs.Issue{State: "open", Body: body}, read, Options{})
	if !v.GoodToGo() || v.Verified != 1 || v.Unmet != 0 {
		t.Fatalf("evidenced (unchecked) criterion should be verified: %+v", v)
	}
	if got := FindingsWith(vcs.Issue{Body: body}, read); len(got) != 0 {
		t.Fatalf("verified criterion must not be reported: %+v", got)
	}
}

// TestExplicitApprovalOverrides allows a reviewer label to stand in for
// per-criterion evidence.
func TestExplicitApprovalOverrides(t *testing.T) {
	issue := vcs.Issue{Number: 9, State: "open", Body: "## Acceptance criteria\n\n- [x] shipped\n"}
	if EvaluateWith(issue, nil).GoodToGo() {
		t.Fatal("without approval the issue must not be good-to-go")
	}
	v := EvaluateWithOptions(issue, nil, Options{Approved: true})
	if !v.GoodToGo() {
		t.Fatalf("explicit approval should allow closure: %+v", v)
	}
}

func TestFindingsNoCriteria(t *testing.T) {
	got := FindingsWith(vcs.Issue{Number: 1, State: "closed", Body: "no list here"}, nil)
	if len(got) != 1 || got[0].Anchor != "no-criteria" || got[0].Severity != findings.P3 {
		t.Fatalf("expected a single P3 no-criteria finding, got %+v", got)
	}
}
