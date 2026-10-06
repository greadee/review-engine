# ADR-0005: Two-phase review for fork safety

## Status

Accepted

## Context

Semantic review needs a provider API key; publishing needs a write token. A pull
request from a fork is untrusted and, on `pull_request`, runs without secrets.
Running the provider call and the write token in the same job that executes the
PR's own tooling risks leaking credentials to untrusted code (ADR-0003).

## Decision

Split a review into two phases with a serializable artifact between them:

1. **Collect** (`revu collect`) — resolves scope, reads file contents, and runs
   the static analyzers and detectors. It performs no model call and needs no
   secrets, so it is safe to run against untrusted code. It writes a `bundle`
   (profile, revisions, run id, file contents, static findings, rubric).
2. **Finalize** (`revu finalize`) — reads the bundle, applies the semantic
   review, merges and normalizes findings, updates tracking, renders the report,
   archives, and comments. It needs the API key and a write token but does **not**
   check out the repository.

`revu run` remains collect + finalize in one process for local use and
same-repository CI. The reusable workflow runs collect and finalize as separate
jobs; for untrusted forks the `collect` and `finalize` workflows are bridged with
`workflow_run` (see `examples/fork-safe/`).

## Consequences

- Untrusted code never shares a job with secrets.
- The bundle is a durable, inspectable record of what the model saw.
- The finalize job is provider-bound and network-bound only; it needs no repo.
- Reruns are stable: the run id is derived from the profile and head, and the
  bundle is deterministic.
