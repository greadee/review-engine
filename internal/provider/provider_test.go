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
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
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
	if gotAuth != "Bearer secret" {
		t.Fatalf("missing bearer auth, got %q", gotAuth)
	}
	if gotBody["model"] != "test-model" {
		t.Fatalf("model not sent: %v", gotBody["model"])
	}
	if _, ok := gotBody["response_format"]; !ok {
		t.Fatal("JSON mode should send response_format")
	}
	if resp.Text != `{"findings":[]}` {
		t.Fatalf("unexpected text %q", resp.Text)
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
