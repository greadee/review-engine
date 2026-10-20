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

	// Non-OpenAI URLs (like LM Studio) default to not supporting json_object
	// — the flag must be set explicitly.
	falseVal := false
	p := NewOpenAI(Config{BaseURL: srv.URL, APIKey: "secret", Model: "test-model", SupportsJSONFormat: &falseVal})
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
		Messages:    []Message{{Role: "user", Content: "hi"}},
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

	// Explicitly disable JSON format — non-OpenAI URLs may reject json_object.
	falseVal := false
	p := NewOpenAI(Config{BaseURL: srv.URL, APIKey: "secret", Model: "test-model", SupportsJSONFormat: &falseVal})
	zero := 0.0
	_, err := p.Complete(context.Background(), Request{
		Messages:    []Message{{Role: "user", Content: "hi"}},
		Temperature: &zero,
		JSON:        true,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	// For non-OpenAI URLs (like local LM Studio), response_format is
	// intentionally omitted because those endpoints reject "json_object".
	if _, ok := gotBody["response_format"]; ok {
		t.Fatal("non-OpenAI URL should not send response_format")
	}
}

func TestOpenAICompleteOmitsResponseFormatWhenSupportsJSONFormatIsFalse(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","choices":[{"message":{"role":"assistant","content":"{\"findings\":[]}"}}],"usage":{"totalTokens":3}}`))
	}))
	defer srv.Close()

	// Explicitly disable JSON format support — response_format should be
	// omitted even for OpenAI URLs when the provider is configured to
	// reject json_object (e.g. LM Studio with reasoning models).
	p := NewOpenAI(Config{BaseURL: srv.URL, APIKey: "secret", Model: "test-model"})
	falseVal := false
	_, err := p.Complete(context.Background(), Request{
		Messages:           []Message{{Role: "user", Content: "hi"}},
		JSON:               true,
		SupportsJSONFormat: &falseVal,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, ok := gotBody["response_format"]; ok {
		t.Fatal("response_format must be omitted when SupportsJSONFormat is false")
	}
}

func TestOpenAICompleteSendsResponseFormatWhenSupportsJSONFormatIsTrue(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","choices":[{"message":{"role":"assistant","content":"{\"findings\":[]}"}}],"usage":{"totalTokens":3}}`))
	}))
	defer srv.Close()

	// When SupportsJSONFormat is true (default), response_format is sent.
	p := NewOpenAI(Config{BaseURL: srv.URL, APIKey: "secret", Model: "test-model"})
	trueVal := true
	_, err := p.Complete(context.Background(), Request{
		Messages:           []Message{{Role: "user", Content: "hi"}},
		JSON:               true,
		SupportsJSONFormat: &trueVal,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	rf, ok := gotBody["response_format"]
	if !ok {
		t.Fatal("response_format must be sent when SupportsJSONFormat is true")
	}
	rfMap, ok := rf.(map[string]any)
	if !ok {
		t.Fatalf("response_format should be an object, got %T", rf)
	}
	if rfMap["type"] != "json_object" {
		t.Fatalf("expected type json_object, got %v", rfMap["type"])
	}
}
