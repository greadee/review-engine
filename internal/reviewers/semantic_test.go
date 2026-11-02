package reviewers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/provider"
)

func TestReviewParsesFindings(t *testing.T) {
	fake := &provider.Fake{Responses: []string{
		"```json\n{\"findings\":[{\"title\":\"Nil deref\",\"classification\":\"Bug\",\"severity\":\"P1\",\"file\":\"x.go\",\"line\":4,\"snippet\":\"f()\",\"impact\":\"crash\",\"recommendation\":\"guard\",\"anchor\":\"nil-deref\"}]}\n```",
	}}
	r := New(fake, "m", 0, nil)
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
	r := New(fake, "m", 0, nil)
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
	r := New(fake, "m", 0, nil)
	got, _, err := r.Review(context.Background(), Input{Files: []FileContent{{Path: "a.go"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Classification != findings.Bug || got[0].Severity != findings.P2 {
		t.Fatalf("expected Bug/P2 fallbacks, got %s/%s", got[0].Classification, got[0].Severity)
	}
}

func TestNewWithLimits(t *testing.T) {
	if r := NewWithLimits(nil, "m", 0, nil, 3, 4000); r.MaxBatchFiles != 3 || r.MaxBatchBytes != 4000 {
		t.Fatalf("explicit limits not applied: %d/%d", r.MaxBatchFiles, r.MaxBatchBytes)
	}
	if r := NewWithLimits(nil, "m", 0, nil, 0, 0); r.MaxBatchFiles != 25 || r.MaxBatchBytes != 150_000 {
		t.Fatalf("default limits changed: %d/%d", r.MaxBatchFiles, r.MaxBatchBytes)
	}
	if r := NewWithLimits(nil, "m", 2048, nil, 0, 0); r.MaxBatchFiles != 8 || r.MaxBatchBytes != 80_000 {
		t.Fatalf("reasoning defaults changed: %d/%d", r.MaxBatchFiles, r.MaxBatchBytes)
	}
	if r := NewWithLimits(nil, "m", 2048, nil, 12, 100_000); r.MaxBatchFiles != 12 || r.MaxBatchBytes != 100_000 {
		t.Fatalf("explicit limits must override reasoning defaults: %d/%d", r.MaxBatchFiles, r.MaxBatchBytes)
	}
}

func TestBatchFilesHonorsByteLimit(t *testing.T) {
	files := []FileContent{
		{Path: "a.go", Content: "12345"},
		{Path: "b.go", Content: "12345"},
		{Path: "c.go", Content: "1"},
	}
	batches := batchFiles(files, 10, 10)
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 1 {
		t.Fatalf("unexpected batches: %+v", batches)
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
	r := New(fake, "m", 0, nil)
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

func TestReviewHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &provider.Fake{Responses: []string{`{"findings":[]}`}}
	r := New(fake, "m", 0, nil)
	_, _, err := r.Review(ctx, Input{Files: []FileContent{{Path: "a.go"}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if fake.Calls() != 0 {
		t.Fatal("provider must not be called after cancellation")
	}
}

func TestReviewStopsWhenProviderCancels(t *testing.T) {
	call := 0
	fake := &provider.Fake{Fn: func(provider.Request) (string, error) {
		call++
		if call == 2 {
			return "", context.Canceled
		}
		return `{"findings":[]}`, nil
	}}
	r := New(fake, "m", 0, nil)
	r.MaxBatchFiles = 1
	_, cov, err := r.Review(context.Background(), Input{Files: []FileContent{{Path: "a.go"}, {Path: "b.go"}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if cov.Analyzed != 1 || cov.Failed != 0 {
		t.Fatalf("unexpected coverage after cancellation: %+v", cov)
	}
}

func TestParseOutputRejectsOversizedInput(t *testing.T) {
	if _, err := parseOutput(strings.Repeat("x", maxParseBytes+1)); err == nil {
		t.Fatal("oversized model output must be rejected")
	}
}

func TestParseOutputBoundedCandidates(t *testing.T) {
	text := strings.Repeat("}", maxParseCandidates+100) + strings.Repeat("{", maxParseCandidates+100)
	if _, err := parseOutput(text); err == nil {
		t.Fatal("pathological brace output must not produce a finding object")
	}
}

func TestParseOutputFindsJSONAfterReasoning(t *testing.T) {
	text := "reasoning with { braces } and prose\n" + `{"findings":[{"title":"ok","file":"a.go"}]}` + "\n"
	out, err := parseOutput(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Findings) != 1 || out.Findings[0].Title != "ok" {
		t.Fatalf("unexpected output: %+v", out)
	}
}

func TestReviewEnforcesRequestTokenBudget(t *testing.T) {
	var got int
	fake := &provider.Fake{Fn: func(req provider.Request) (string, error) {
		got = req.MaxTokens
		return `{"findings":[]}`, nil
	}}
	r := NewWithLimits(fake, "m", 2048, nil, 0, 0)
	if _, _, err := r.Review(context.Background(), Input{Files: []FileContent{{Path: "a.go"}}}); err != nil {
		t.Fatal(err)
	}
	if got < 8192+2048 {
		t.Fatalf("request max tokens = %d, expected reasoning budget included", got)
	}
}

func TestReviewRecordsRetryExhaustionAsFailure(t *testing.T) {
	fake := &provider.Fake{Fn: func(provider.Request) (string, error) {
		return "", fmt.Errorf("%w: server", provider.ErrRetryExhausted)
	}}
	r := New(fake, "m", 0, nil)
	_, cov, err := r.Review(context.Background(), Input{Files: []FileContent{{Path: "a.go"}}})
	if err != nil {
		t.Fatal(err)
	}
	if cov.Failed != 1 || len(cov.Errors) != 1 || !strings.Contains(cov.Errors[0], "retry exhausted") {
		t.Fatalf("retry exhaustion was not recorded: %+v", cov)
	}
}

func TestReviewRejectsPathologicalResponse(t *testing.T) {
	fake := &provider.Fake{Responses: []string{strings.Repeat("{", maxParseBytes+1)}}
	r := New(fake, "m", 0, nil)
	_, cov, err := r.Review(context.Background(), Input{Files: []FileContent{{Path: "a.go"}}})
	if err != nil {
		t.Fatal(err)
	}
	if cov.Failed != 1 {
		t.Fatalf("pathological response should fail the batch: %+v", cov)
	}
}
