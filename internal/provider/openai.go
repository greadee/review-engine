package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the OpenAI public API. Any OpenAI-compatible endpoint may
// be substituted (DeepSeek, vLLM, Ollama, ...).
const DefaultBaseURL = "https://api.openai.com/v1"

// OpenAI is a provider for any OpenAI-compatible chat-completions endpoint.
type OpenAI struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// NewOpenAI builds an OpenAI-compatible provider. An empty BaseURL or Model
// falls back to the default / the request's model.
func NewOpenAI(cfg Config) *OpenAI {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	return &OpenAI{
		baseURL: base,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// Name implements Provider.
func (o *OpenAI) Name() string { return "openai-compatible" }

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete implements Provider against POST {base}/chat/completions.
func (o *OpenAI) Complete(ctx context.Context, req Request) (Response, error) {
	model := req.Model
	if model == "" {
		model = o.model
	}
	if model == "" {
		return Response{}, fmt.Errorf("provider: model is required")
	}
	body := chatRequest{
		Model:       model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if req.JSON {
		body.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("provider: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("provider: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("provider: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Response{}, fmt.Errorf("provider: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("provider: status %d: %s", resp.StatusCode, truncate(string(data), 512))
	}
	var parsed chatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Response{}, fmt.Errorf("provider: decode response: %w", err)
	}
	if parsed.Error != nil {
		return Response{}, fmt.Errorf("provider: %s: %s", parsed.Error.Type, parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("provider: empty choices")
	}
	return Response{
		Text:  parsed.Choices[0].Message.Content,
		Usage: parsed.Usage,
		Model: parsed.Model,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
