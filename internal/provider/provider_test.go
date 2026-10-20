package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIComplete(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","choices":[{"message":{"role":"assistant","content":"{\"findings\":[]}"}}],"usage":{"totalTokens":3}}`))
	}))
	defer srv.Close()

	p := NewOpenAI(Config{BaseURL: srv.URL, APIKey: "secret", Model: "test-model"})
	zero := 0.0
	resp, err := p.Complete(context.Background(), Request{
		Messages:    []Message{{Role: "user", Content: "hi"}},
		Temperature: &zero,
		JSON:        true,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Text != `{"findings":[]}` {
		t.Fatalf("unexpected text %q", resp.Text)
	}
	// For non-OpenAI URLs (like local LM Studio), response_format is
	// intentionally omitted because those endpoints reject "json_object".
	if _, ok := gotBody["response_format"]; ok {
		t.Fatal("non-OpenAI URL should not send response_format")
	}
}

func TestOpenAICompleteSendsMaxReasoningTokens(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","choices":[{"message":{"role":"assistant","content":"{\"findings\":[]}"}}],"usage":{"totalTokens":3}}`))
	}))
	defer srv.Close()

	p := NewOpenAI(Config{BaseURL: srv.URL, APIKey: "secret", Model: "test-model"})
	zero := 0.0
	_, err := p.Complete(context.Background(), Request{
		Messages:           []Message{{Role: "user", Content: "hi"}},
		Temperature:        &zero,
		MaxReasoningTokens: 2048,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	mrt, ok := gotBody["max_reasoning_tokens"]
	if !ok {
		t.Fatal("max_reasoning_tokens not sent when > 0")
	}
	if mrt.(float64) != 2048 {
		t.Fatalf("expected max_reasoning_tokens=2048, got %v", mrt)
	}
}

func TestOpenAICompleteOmitsMaxReasoningTokensWhenZero(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","choices":[{"message":{"role":"assistant","content":"{\"findings\":[]}"}}],"usage":{"totalTokens":3}}`))
	}))
	defer srv.Close()

	p := NewOpenAI(Config{BaseURL: srv.URL, APIKey: "secret", Model: "test-model"})
	zero := 0.0
	_, err := p.Complete(context.Background(), Request{
		Messages:  []Message{{Role: "user", Content: "hi"}},
		Temperature: &zero,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, ok := gotBody["max_reasoning_tokens"]; ok {
		t.Fatal("max_reasoning_tokens should be omitted when 0")
	}
}

func TestFakeOrderingAndRegistry(t *testing.T) {
	f := &Fake{Responses: []string{"one", "two"}}
	for i, want := range []string{"one", "two"} {
		resp, err := f.Complete(context.Background(), Request{})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if resp.Text != want {
			t.Fatalf("call %d: got %q want %q", i, resp.Text, want)
		}
	}
	if _, err := New("nope", Config{}); err == nil {
		t.Fatal("expected unknown provider error")
	}
	if _, err := New("fake", Config{}); err != nil {
		t.Fatalf("fake should be registered: %v", err)
	}
}

func TestOpenAICompleteNonOpenAIURL(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","choices":[{"message":{"role":"assistant","content":"{\"findings\":[]}"}}],"usage":{"totalTokens":3}}`))
	}))
	defer srv.Close()

	p := NewOpenAI(Config{BaseURL: srv.URL, APIKey: "secret", Model: "test-model"})
	zero := 0.0
	_, err := p.Complete(context.Background(), Request{
		Messages:  []Message{{Role: "user", Content: "hi"}},
		Temperature: &zero,
		JSON:      true,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, ok := gotBody["response_format"]; ok {
		t.Fatal("non-OpenAI URL should not send response_format")
	}
}

func TestOpenAICompleteSendsMaxReasoningTokens(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","choices":[{"message":{"role":"assistant","content":"{\"findings\":[]}"}}],"usage":{"totalTokens":3}}`))
	}))
	defer srv.Close()

	p := NewOpenAI(Config{BaseURL: srv.URL, APIKey: "secret", Model: "test-model"})
	zero := 0.0
	_, err := p.Complete(context.Background(), Request{
		Messages:           []Message{{Role: "user", Content: "hi"}},
		Temperature:        &zero,
		MaxReasoningTokens: 2048,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	mrt, ok := gotBody["max_reasoning_tokens"]
	if !ok {
		t.Fatal("max_reasoning_tokens not sent when > 0")
	}
	if mrt.(float64) != 2048 {
		t.Fatalf("expected max_reasoning_tokens=2048, got %v", mrt)
	}
}

func TestOpenAICompleteOmitsMaxReasoningTokensWhenZero(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","choices":[{"message":{"role":"assistant","content":"{\"findings\":[]}"}}],"usage":{"totalTokens":3}}`))
	}))
	defer srv.Close()

	p := NewOpenAI(Config{BaseURL: srv.URL, APIKey: "secret", Model: "test-model"})
	zero := 0.0
	_, err := p.Complete(context.Background(), Request{
		Messages:  []Message{{Role: "user", Content: "hi"}},
		Temperature: &zero,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, ok := gotBody["max_reasoning_tokens"]; ok {
		t.Fatal("max_reasoning_tokens should be omitted when 0")
	}
}
