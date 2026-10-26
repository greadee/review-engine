# Changelog

All notable changes to this project are recorded here. Release titles use the
animal-rank nickname; the release purpose is the theme of the release notes.

## [1.1.1] - 2026-10-20

**Common Rat** — post-v1 hardening patch.

Release commit: `30e90d550f0fab6e0c3e05a95e4a98b08838af31`

### Added

- Reasoning-model support with `maxReasoningTokens`.
- `supportsJsonFormat` to omit `response_format` for compatible endpoints that
  reject `json_object`.
- A PR-stable summary-comment marker.

### Changed

- Whole-repository `audit` and `sprint` scans no longer inherit the PR file cap.
- Archive JSON uses consistent field names.
- `--dry-run` no longer writes tracking state.
- A configured GitHub token is respected by `run`, `finalize`, and `issue`.

### Fixed

- Finding evidence is verified by file and line, so static analyzer findings can
  block correctly.
- Analyzer subprocesses run with a scrubbed environment and do not inherit the
  provider key or GitHub token.
- Python compile failures carry file and line evidence.
- Reasoning-model batch handling no longer fails on providers that reject
  `response_format`.

### Known issues

- Provider retry/backoff is not included in this patch.
- Comment upsert calls `GET /user`; tokens without user-scope access can receive
  a 403. The default-token path is completed in `v1.2.0`.
- The CLI version string is still hard-coded as `0.1.0`; it is corrected in
  `v1.2.0`.
- The committed `revu.yaml` is a local development configuration and must not be
  copied into a consumer repository.
- GitHub Enterprise permalinks are not supported.
- Issue evidence remains reviewable, not proof.

## [1.0.1] - 2026-10-15

Immutable findings link and fork-flow proof.

- Archive links use a commit-pinned permalink.
- Fork-safe collect/finalize integration harness.
- Consumer install and known-limitations documentation.
- Examples and reusable workflow pinned to reviewed revisions.

## [1.0.0] - 2026-10-14

Initial stable release: safe, deterministic PR review.

- PR, audit, and issue profiles end to end.
- Static analyzers, semantic review, tracking, archiving, and reporting.
- Fork-safe two-phase review flow.
