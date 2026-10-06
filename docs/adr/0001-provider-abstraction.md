# ADR-0001: Provider abstraction

## Status

Accepted

## Context

The engine needs a language model for the semantic review pass. Vendor SDKs differ
and change; tying the pipeline to one vendor would make the engine hard to run in
different environments (cloud, self-hosted, local).

## Decision

Define a single `provider.Provider` interface (`Complete(ctx, Request) (Response, error)`)
plus a name-keyed registry and factory. The first concrete implementation is
`openai-compatible`, which covers OpenAI and any compatible endpoint (DeepSeek, vLLM,
Ollama) via a configurable base URL. A `fake` provider supports tests and `--dry-run`.

The engine depends only on the interface. Adding a provider is a new package plus a
`Register` call; no pipeline changes.

## Consequences

- The engine is provider-agnostic and testable without network access.
- Determinism is controlled by the caller: temperature 0, pinned model.
- Provider-specific features (tool use, caching headers) are not exposed until needed;
  they can be added to the `Request`/`Response` types without breaking the interface.
