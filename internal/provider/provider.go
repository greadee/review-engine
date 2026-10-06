// Package provider abstracts the language model used for semantic review. A
// single interface plus a registry keeps the engine provider-agnostic; only
// openai-compatible is implemented today.
package provider

import (
	"context"
	"fmt"
)

// Message is one chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is a completion request. Temperature is a pointer so a caller can
// distinguish "unset" from "0.0"; the engine uses 0 for determinism.
type Request struct {
	Messages    []Message
	Model       string
	Temperature *float64
	MaxTokens   int
	JSON        bool
}

// Usage reports token consumption.
type Usage struct {
	PromptTokens     int `json:"promptTokens,omitempty"`
	CompletionTokens int `json:"completionTokens,omitempty"`
	TotalTokens      int `json:"totalTokens,omitempty"`
}

// Response is a completion result.
type Response struct {
	Text  string
	Usage Usage
	Model string
}

// Provider completes prompts.
type Provider interface {
	// Name returns the registered provider name.
	Name() string
	// Complete performs a completion. Implementations must honour ctx.
	Complete(ctx context.Context, req Request) (Response, error)
}

// Config configures a provider factory.
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
}

// Factory constructs a provider from configuration.
type Factory func(Config) (Provider, error)

// ErrUnknownProvider is returned when a name is not registered.
var ErrUnknownProvider = fmt.Errorf("provider: unknown provider")
