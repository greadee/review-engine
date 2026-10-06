package provider

import (
	"fmt"
	"sort"
	"sync"
)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register adds a provider factory. It is intended to be called from init in
// provider implementations.
func Register(name string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

// New constructs a provider by name.
func New(name string, cfg Config) (Provider, error) {
	registryMu.RLock()
	factory, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, name)
	}
	return factory(cfg)
}

// Names returns the registered provider names, sorted.
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func init() {
	Register("openai-compatible", func(cfg Config) (Provider, error) {
		return NewOpenAI(cfg), nil
	})
	Register("fake", func(cfg Config) (Provider, error) {
		return &Fake{}, nil
	})
}
