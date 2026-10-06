# ADR-0002: Findings schema and fingerprint

## Status

Accepted

## Context

Findings must survive a session and be tracked across runs. If identity depends on
model-generated prose, rewording a finding would make it appear resolved and then
reappear, churning lifecycle state and noise.

## Decision

A finding is a stable record: title, classification, severity (P0–P3), evidence
(`file:line` + snippet), impact, recommendation, scope, suggested phase, profile,
detector, and lifecycle status.

Identity is a **fingerprint** computed from `(normalized path, detector, anchor)`,
where `anchor` is a short, stable slug supplied by the detector or the model. The
fingerprint deliberately excludes free-form prose such as the title, impact, or
recommendation.

Lifecycle reconciles each run against the previous state: unknown fingerprints are
`new`, known are `ongoing`, disappeared are `resolved`, and a resolved finding that
reappears is `regressed`.

## Consequences

- Findings are stable across rewordings and line shifts that keep the same anchor.
- Detectors must supply meaningful anchors; a missing anchor falls back to the title.
- Severity is merged toward the more severe of duplicates with the same fingerprint.
