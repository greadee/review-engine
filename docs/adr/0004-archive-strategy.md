# ADR-0004: Archive strategy

## Status

Accepted

## Context

Findings must be durable and linkable from a PR summary, without polluting the
repository's normal history or the PR branch. CI artifacts expire and require
authentication to open.

## Decision

Archive each run as two files — `<dir>/<runID>.json` and `<dir>/<runID>.md` — on a
dedicated `review-artifacts` branch, committed via the GitHub contents API. The PR
comment links to the JSON file at a commit-pinned permalink
(`https://github.com/<owner>/<repo>/blob/<branch>/<path>`). The engine never writes to
the default or PR branch.

`runID` is `<profile>@<short-head-sha>`, so identical reruns map to the same artifact
path and reruns are effectively no-ops.

## Consequences

- Links are stable and survive artifact expiry.
- The bot branch accumulates one small pair of files per run; consumers may prune it.
- Writing requires a token with `contents: write`.
- `archive.enabled: false` disables publishing entirely.
