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
	Provider  provider.Provider
	Model     string
	MaxTokens int
}

// New builds a semantic reviewer.
func New(p provider.Provider, model string) *Semantic {
	return &Semantic{Provider: p, Model: model, MaxTokens: 4096}
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

// Review calls the provider and returns findings. When there are no files it
// returns no findings without calling the provider.
func (s *Semantic) Review(ctx context.Context, in Input) ([]findings.Finding, error) {
	if s.Provider == nil {
		return nil, fmt.Errorf("reviewer: provider is required")
	}
	if len(in.Files) == 0 {
		return nil, nil
	}
	req := provider.Request{
		Model:       s.Model,
		MaxTokens:   s.MaxTokens,
		JSON:        true,
		Temperature: zero(),
		Messages: []provider.Message{
			{Role: "system", Content: systemPrompt()},
			{Role: "user", Content: userPrompt(in)},
		},
	}
	resp, err := s.Provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("reviewer: completion failed: %w", err)
	}
	parsed, err := parseOutput(resp.Text)
	if err != nil {
		return nil, err
	}
	out := make([]findings.Finding, 0, len(parsed.Findings))
	for _, mf := range parsed.Findings {
		out = append(out, toFinding(mf, in.Profile))
	}
	return out, nil
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
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return modelOutput{}, fmt.Errorf("reviewer: model returned no JSON object")
	}
	var out modelOutput
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return modelOutput{}, fmt.Errorf("reviewer: decode model output: %w", err)
	}
	return out, nil
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
