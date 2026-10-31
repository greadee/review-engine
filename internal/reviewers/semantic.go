// Package reviewers performs model-backed semantic review of source files
// against a rubric and converts the model's structured output into findings.
package reviewers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/provider"
)

// FileContent is one file supplied for review.
type FileContent struct {
	Path    string
	Content string
}

// Input is a semantic review request.
type Input struct {
	Profile string
	Rubric  []string
	Files   []FileContent
}

// Semantic is a model-backed reviewer.
type Semantic struct {
	Provider           provider.Provider
	Model              string
	MaxTokens          int
	MaxBatchFiles      int
	MaxBatchBytes      int
	MaxReasoningTokens int
	// SupportsJSONFormat controls whether response_format is sent.
	// nil means the provider default applies.  false means omit it.
	SupportsJSONFormat *bool
}

// New builds a semantic reviewer with automatic batching limits.
func New(p provider.Provider, model string, maxReasoningTokens int, supportsJSONFormat *bool) *Semantic {
	return NewWithLimits(p, model, maxReasoningTokens, supportsJSONFormat, 0, 0)
}

// NewWithLimits builds a semantic reviewer with explicit batching limits. A
// non-positive batchFiles or batchBytes keeps the automatic default for the
// model: 25 files / 150000 bytes, or 8 files / 80000 bytes when reasoning
// tokens are configured.
func NewWithLimits(p provider.Provider, model string, maxReasoningTokens int, supportsJSONFormat *bool, batchFiles, batchBytes int) *Semantic {
	// Base budget of 8192 tokens is enough for non-reasoning models.
	// When reasoning tokens are configured, add them to the budget so the
	// model has room for reasoning + JSON output.
	maxTokens := 8192
	if maxReasoningTokens > 0 {
		maxTokens += maxReasoningTokens
	}
	// Reasoning models produce verbose reasoning text; use smaller batches
	// so the JSON output fits within the token budget.
	maxBatchFiles := 25
	maxBatchBytes := 150_000
	if maxReasoningTokens > 0 {
		maxBatchFiles = 8
		maxBatchBytes = 80_000
	}
	if batchFiles > 0 {
		maxBatchFiles = batchFiles
	}
	if batchBytes > 0 {
		maxBatchBytes = batchBytes
	}
	return &Semantic{Provider: p, Model: model, MaxTokens: maxTokens, MaxBatchFiles: maxBatchFiles, MaxBatchBytes: maxBatchBytes, MaxReasoningTokens: maxReasoningTokens, SupportsJSONFormat: supportsJSONFormat}
}

// Coverage reports how a semantic pass was executed. Analyzed counts files in
// batches that completed; Failed counts batches that errored.
type Coverage struct {
	Batches  int
	Analyzed int
	Failed   int
	Errors   []string
}

type modelFinding struct {
	Title          string `json:"title"`
	Classification string `json:"classification"`
	Severity       string `json:"severity"`
	File           string `json:"file"`
	Line           int    `json:"line"`
	Snippet        string `json:"snippet"`
	Impact         string `json:"impact"`
	Recommendation string `json:"recommendation"`
	Anchor         string `json:"anchor"`
}

type modelOutput struct {
	Findings []modelFinding `json:"findings"`
}

// Review calls the provider in bounded batches and returns the findings plus
// coverage. When there are no files it returns no findings without calling the
// provider. A batch that fails is recorded in coverage rather than aborting the
// run, so a failure produces an incomplete result instead of a silent approve.
func (s *Semantic) Review(ctx context.Context, in Input) ([]findings.Finding, Coverage, error) {
	if s.Provider == nil {
		return nil, Coverage{}, fmt.Errorf("reviewer: provider is required")
	}
	if len(in.Files) == 0 {
		return nil, Coverage{}, nil
	}
	batches := batchFiles(in.Files, s.batchFiles(), s.batchBytes())
	var out []findings.Finding
	cov := Coverage{Batches: len(batches)}
	for _, batch := range batches {
		req := provider.Request{
			Model:              s.Model,
			MaxTokens:          s.MaxTokens,
			MaxReasoningTokens: s.MaxReasoningTokens,
			JSON:               true,
			SupportsJSONFormat: s.SupportsJSONFormat,
			Temperature:        zero(),
			Messages: []provider.Message{
				{Role: "system", Content: systemPrompt()},
				{Role: "user", Content: userPrompt(Input{Profile: in.Profile, Rubric: in.Rubric, Files: batch})},
			},
		}
		resp, err := s.Provider.Complete(ctx, req)
		if err != nil {
			cov.Failed++
			cov.Errors = append(cov.Errors, err.Error())
			continue
		}
		parsed, err := parseOutput(resp.Text)
		if err != nil {
			cov.Failed++
			cov.Errors = append(cov.Errors, err.Error())
			continue
		}
		cov.Analyzed += len(batch)
		for _, mf := range parsed.Findings {
			out = append(out, toFinding(mf, in.Profile))
		}
	}
	return out, cov, nil
}

func (s *Semantic) batchFiles() int {
	if s.MaxBatchFiles > 0 {
		return s.MaxBatchFiles
	}
	return 25
}

func (s *Semantic) batchBytes() int {
	if s.MaxBatchBytes > 0 {
		return s.MaxBatchBytes
	}
	return 150_000
}

// batchFiles splits files into batches bounded by count and total bytes. A
// single file larger than the byte budget gets its own batch.
func batchFiles(files []FileContent, maxFiles, maxBytes int) [][]FileContent {
	var batches [][]FileContent
	var cur []FileContent
	curBytes := 0
	for _, f := range files {
		sz := len(f.Content)
		if len(cur) > 0 && (len(cur) >= maxFiles || curBytes+sz > maxBytes) {
			batches = append(batches, cur)
			cur = nil
			curBytes = 0
		}
		cur = append(cur, f)
		curBytes += sz
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	return batches
}

func toFinding(mf modelFinding, profile string) findings.Finding {
	f := findings.Finding{
		Title:          strings.TrimSpace(mf.Title),
		Classification: classification(mf.Classification),
		Severity:       severity(mf.Severity),
		Evidence:       findings.Evidence{File: strings.TrimPrefix(strings.TrimSpace(mf.File), "./"), Line: mf.Line, Snippet: strings.TrimSpace(mf.Snippet)},
		Impact:         strings.TrimSpace(mf.Impact),
		Recommendation: strings.TrimSpace(mf.Recommendation),
		Profile:        profile,
		Detector:       "semantic." + strings.ToLower(strings.TrimSpace(mf.Classification)),
		Anchor:         strings.TrimSpace(mf.Anchor),
	}
	if f.Anchor == "" {
		f.Anchor = f.Title
	}
	return f
}

func classification(s string) findings.Classification {
	c := findings.Classification(strings.TrimSpace(s))
	if c.Valid() {
		return c
	}
	// Tolerate case/spacing variants.
	for _, known := range []findings.Classification{
		findings.Bug, findings.TechnicalDebt, findings.Refactor, findings.Security,
		findings.Test, findings.Documentation, findings.Architecture, findings.Reliability,
		findings.Performance, findings.Maintainability, findings.Data,
	} {
		if strings.EqualFold(string(known), strings.TrimSpace(s)) {
			return known
		}
	}
	return findings.Bug
}

func severity(s string) findings.Severity {
	sev := findings.Severity(strings.ToUpper(strings.TrimSpace(s)))
	if sev.Valid() {
		return sev
	}
	return findings.P2
}

func parseOutput(text string) (modelOutput, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")

	// For reasoning models the text may contain reasoning discussion before
	// the JSON.  Search for the last valid JSON object rather than blindly
	// taking the first "{" to the last "}" (which can span reasoning text
	// and produce invalid JSON).
	for i := len(text) - 1; i >= 0; i-- {
		if text[i] != '}' {
			continue
		}
		// Look for the matching "{" before this "}"
		for j := i - 1; j >= 0; j-- {
			if text[j] != '{' {
				continue
			}
			candidate := text[j : i+1]
			var out modelOutput
			if json.Unmarshal([]byte(candidate), &out) == nil {
				return out, nil
			}
		}
	}
	return modelOutput{}, fmt.Errorf("reviewer: model returned no JSON object")
}

func zero() *float64 {
	v := 0.0
	return &v
}

func systemPrompt() string {
	return `You are a rigorous senior code reviewer performing a structured review.
Review only the supplied files. Report concrete, evidence-backed findings — never speculate or pad.
Return a single JSON object and nothing else, matching this schema:
{"findings":[{"title":string,"classification":string,"severity":string,"file":string,"line":number,"snippet":string,"impact":string,"recommendation":string,"anchor":string}]}
Allowed classification: Bug, Technical Debt, Refactor, Security, Test, Documentation, Architecture, Reliability, Performance, Maintainability, Data.
Allowed severity: P0 (critical), P1 (high), P2 (medium), P3 (low).
"anchor" must be a short stable slug identifying the specific issue (for example "nil-policy-fail-open"), not a sentence.
If there are no findings, return {"findings":[]}.`
}

func userPrompt(in Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Review profile: %s\n", in.Profile)
	fmt.Fprintf(&b, "Review aspects: %s\n\n", strings.Join(in.Rubric, ", "))
	for _, f := range in.Files {
		fmt.Fprintf(&b, "=== FILE: %s ===\n%s\n\n", f.Path, f.Content)
	}
	return b.String()
}
