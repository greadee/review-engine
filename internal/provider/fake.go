package provider

import (
	"context"
	"fmt"
	"sync"
)

// Fake is an in-memory provider for tests and dry runs. It returns queued
// responses in order; when Fn is set it takes precedence. It is safe for
// concurrent use.
type Fake struct {
	// Responses are returned in order. When exhausted, the last is repeated.
	Responses []string
	// Fn, when set, computes the response for each request.
	Fn func(Request) (string, error)

	mu    sync.Mutex
	calls int
	seen  []Request
}

// Name implements Provider.
func (f *Fake) Name() string { return "fake" }

// Calls returns the number of completions served.
func (f *Fake) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// Requests returns a copy of the requests served.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Request, len(f.seen))
	copy(out, f.seen)
	return out
}

// Complete implements Provider.
func (f *Fake) Complete(_ context.Context, req Request) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.seen = append(f.seen, req)
	if f.Fn != nil {
		text, err := f.Fn(req)
		if err != nil {
			return Response{}, err
		}
		return Response{Text: text, Model: f.Name()}, nil
	}
	if len(f.Responses) == 0 {
		return Response{}, fmt.Errorf("provider: fake has no responses queued")
	}
	i := f.calls - 1
	if i >= len(f.Responses) {
		i = len(f.Responses) - 1
	}
	return Response{Text: f.Responses[i], Model: f.Name()}, nil
}
