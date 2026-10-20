package reviewers

import (
	"context"
	"fmt"
	"testing"

	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/provider"
)

func TestReviewParsesFindings(t *testing.T) {
	fake := &provider.Fake{Responses: []string{
		"```json\n{\"findings\":[{\"title\":\"Nil deref\",\"classification\":\"Bug\",\"severity\":\"P1\",\"file\":\"x.go\",\"line\":4,\"snippet\":\"f()\",\"impact\":\"crash\",\"recommendation\":\"guard\",\"anchor\":\"nil-deref\"}]}\n```",
	}}
	r := New(fake, "m", 0)
	got, cov, err := r.Review(context.Background(), Input{
		Profile: "pr",
		Rubric:  []string{"correctness"},
		Files:   []FileContent{{Path: "x.go", Content: "package x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	if cov.Analyzed != 1 || cov.Failed != 0 {
		t.Fatalf("unexpected coverage: %+v", cov)
	}
	f := got[0]
	if f.Severity != findings.P1 || f.Classification != findings.Bug || f.Evidence.Line != 4 || f.Anchor != "nil-deref" {
		t.Fatalf("bad finding: %+v", f)
	}
	if f.Detector != "semantic.bug" {
		t.Fatalf("unexpected detector %q", f.Detector)
	}
}

func TestReviewNoFilesSkipsProvider(t *testing.T) {
	fake := &provider.Fake{}
	r := New(fake, "m", 0)
	got, _, err := r.Review(context.Background(), Input{})
	if err != nil || len(got) != 0 {
		t.Fatalf("expected no-op: %v %v", got, err)
	}
	if fake.Calls() != 0 {
		t.Fatal("provider should not be called with no files")
	}
}

func TestReviewFallbacks(t *testing.T) {
	fake := &provider.Fake{Responses: []string{`{"findings":[{"title":"weird","classification":"Nonsense","severity":"SEV1","file":"a.go"}]}`}}
	r := New(fake, "m", 0)
	got, _, err := r.Review(context.Background(), Input{Files: []FileContent{{Path: "a.go"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Classification != findings.Bug || got[0].Severity != findings.P2 {
		t.Fatalf("expected Bug/P2 fallbacks, got %s/%s", got[0].Classification, got[0].Severity)
	}
}

func TestReviewBatchesAndRecordsFailures(t *testing.T) {
	call := 0
	fake := &provider.Fake{Fn: func(provider.Request) (string, error) {
		call++
		if call == 2 {
			return "", fmt.Errorf("boom")
		}
		return `{"findings":[]}`, nil
	}}
	r := New(fake, "m", 0)
	r.MaxBatchFiles = 1 // one file per batch
	files := []FileContent{{Path: "a.go"}, {Path: "b.go"}, {Path: "c.go"}}
	_, cov, err := r.Review(context.Background(), Input{Files: files})
	if err != nil {
		t.Fatal(err)
	}
	if cov.Batches != 3 || cov.Failed != 1 || cov.Analyzed != 2 {
		t.Fatalf("unexpected coverage: %+v", cov)
	}
}
