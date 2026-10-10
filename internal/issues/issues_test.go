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

func TestCollectCriteriaFromDocs(t *testing.T) {
	body := "Docs: `docs/imp.md`\n\nSprint stuff.\n"
	read := func(p string) (string, bool) {
		if p == "docs/imp.md" {
			return "## Acceptance Criteria\n\n- [x] first\n- [ ] second\n", true
		}
		return "", false
	}
	criteria := CollectCriteria(body, read)
	if len(criteria) != 2 {
		t.Fatalf("expected 2 criteria from the doc, got %+v", criteria)
	}
	v := EvaluateWith(vcs.Issue{Number: 1, State: "open", Body: body}, read)
	if !v.HasCriteria || v.Met != 1 || v.Unmet != 1 || v.GoodToGo() {
		t.Fatalf("unexpected verdict: %+v", v)
	}
	// Without a reader, only the body is consulted (none here).
	if got := EvaluateWith(vcs.Issue{Body: body}, nil); got.HasCriteria {
		t.Fatalf("expected no criteria without a reader, got %+v", got)
	}
}

func TestEvaluate(t *testing.T) {
	v := EvaluateWith(vcs.Issue{State: "open", Body: body}, nil)
	if !v.HasCriteria || v.Criteria != 4 || v.Met != 2 || v.Unmet != 2 {
		t.Fatalf("unexpected verdict: %+v", v)
	}
	if v.GoodToGo() {
		t.Fatal("issue with unmet criteria must not be good-to-go")
	}

	gtg := EvaluateWith(vcs.Issue{State: "open", Body: "## Acceptance criteria\n\n- [x] done\n- [x] also done\n"}, nil)
	if !gtg.GoodToGo() || gtg.Unmet != 0 {
		t.Fatalf("expected good-to-go: %+v", gtg)
	}

	none := EvaluateWith(vcs.Issue{Body: "no list here"}, nil)
	if none.HasCriteria || none.GoodToGo() {
		t.Fatalf("no criteria must not be good-to-go: %+v", none)
	}
}

func TestFindingsNoCriteria(t *testing.T) {
	got := FindingsWith(vcs.Issue{Number: 1, State: "closed", Body: "no list here"}, nil)
	if len(got) != 1 || got[0].Anchor != "no-criteria" || got[0].Severity != findings.P3 {
		t.Fatalf("expected a single P3 no-criteria finding, got %+v", got)
	}
}
