package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the OpenAI public API. Any OpenAI-compatible endpoint may
// be substituted (DeepSeek, vLLM, Ollama, ...).
const DefaultBaseURL = "https://api.openai.com/v1"

// OpenAI is a provider for any OpenAI-compatible chat-completions endpoint.
type OpenAI struct {
	baseURL            string
	apiKey             string
	model              string
	supportsJSONFormat bool
	http               *http.Client
	maxAttempts        int
	sleep              func(context.Context, time.Duration) error
}

// NewOpenAI builds an OpenAI-compatible provider. An empty BaseURL or Model
// falls back to the default / the request's model.
func NewOpenAI(cfg Config) *OpenAI {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	// SupportsJSONFormat defaults to true when not set (nil).
	supportsJSON := true
	if cfg.SupportsJSONFormat != nil {
		supportsJSON = *cfg.SupportsJSONFormat
	}
	// Use a longer timeout when reasoning tokens are configured, since
	// reasoning models need more time to produce their output.
	timeout := 120 * time.Second
	if cfg.MaxReasoningTokens > 0 {
		timeout = 300 * time.Second
	}
	return &OpenAI{
		baseURL:            base,
		apiKey:             cfg.APIKey,
		model:              cfg.Model,
		supportsJSONFormat: supportsJSON,
		http:               &http.Client{Timeout: timeout},
		maxAttempts:        4,
		sleep:              sleepContext,
	}
}

// Name implements Provider.
func (o *OpenAI) Name() string { return "openai-compatible" }

type chatRequest struct {
	Model              string          `json:"model"`
	Messages           []Message       `json:"messages"`
	Temperature        *float64        `json:"temperature,omitempty"`
	MaxTokens          int             `json:"max_tokens,omitempty"`
	ResponseFormat     *responseFormat `json:"response_format,omitempty"`
	MaxReasoningTokens int             `json:"max_reasoning_tokens,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatMessage struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message chatMessage `json:"message"`
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
		Model:              model,
		Messages:           req.Messages,
		Temperature:        req.Temperature,
		MaxTokens:          req.MaxTokens,
		MaxReasoningTokens: req.MaxReasoningTokens,
	}
	// Only send response_format for providers that support it.  Providers
	// that reject "json_object" (LM Studio, vLLM, Ollama, reasoning models)
	// set SupportsJSONFormat to false; the system prompt already instructs
	// the model to return JSON, so we omit the field for those endpoints.
	// When the request explicitly sets SupportsJSONFormat (non-nil), honour
	// that override regardless of the provider's default.
	supports := o.supportsJSONFormat
	if req.SupportsJSONFormat != nil {
		supports = *req.SupportsJSONFormat
	}
	if req.JSON && supports {
		body.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("provider: encode request: %w", err)
	}
	attempts := o.maxAttempts
	if attempts < 1 {
		attempts = 1
	}
	sleep := o.sleep
	if sleep == nil {
		sleep = sleepContext
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
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
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return Response{}, fmt.Errorf("provider: request failed: %w", err)
			}
			lastErr = fmt.Errorf("request failed: %w", err)
			if attempt == attempts-1 {
				break
			}
			if err := sleep(ctx, retryDelay(attempt, 0)); err != nil {
				return Response{}, fmt.Errorf("provider: retry wait: %w", err)
			}
			continue
		}

		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		status := resp.StatusCode
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read response: %w", readErr)
			if attempt == attempts-1 {
				break
			}
			if err := sleep(ctx, retryDelay(attempt, 0)); err != nil {
				return Response{}, fmt.Errorf("provider: retry wait: %w", err)
			}
			continue
		}

		if status < 200 || status >= 300 {
			lastErr = fmt.Errorf("status %d: %s", status, truncate(string(data), 512))
			if !retryableStatus(status) {
				return Response{}, fmt.Errorf("provider: %w", lastErr)
			}
			if attempt == attempts-1 {
				break
			}
			if err := sleep(ctx, retryDelay(attempt, retryAfter)); err != nil {
				return Response{}, fmt.Errorf("provider: retry wait: %w", err)
			}
			continue
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
		text := parsed.Choices[0].Message.Content
		if text == "" && parsed.Choices[0].Message.ReasoningContent != "" {
			text = parsed.Choices[0].Message.ReasoningContent
		}
		return Response{
			Text:  text,
			Usage: parsed.Usage,
			Model: parsed.Model,
		}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("request failed")
	}
	return Response{}, fmt.Errorf("provider: request failed after %d attempt(s): %w", attempts, lastErr)
}

func retryableStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	delay := 250 * time.Millisecond << attempt
	if delay > 5*time.Second {
		delay = 5 * time.Second
	}
	return delay
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
