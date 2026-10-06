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
	got := Findings(vcs.Issue{Number: 7, State: "closed", Body: body})
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
	got := Findings(vcs.Issue{Number: 7, State: "open", Body: body})
	for _, f := range got {
		if f.Severity == findings.P1 {
			t.Fatalf("open issue should not produce P1: %+v", f)
		}
	}
}

func TestFindingsNoCriteria(t *testing.T) {
	got := Findings(vcs.Issue{Number: 1, State: "closed", Body: "no list here"})
	if len(got) != 1 || got[0].Anchor != "no-criteria" || got[0].Severity != findings.P3 {
		t.Fatalf("expected a single P3 no-criteria finding, got %+v", got)
	}
}
