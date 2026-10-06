# Review Engine (`revu`) — Development Sprint Plan

> General-purpose code review / PR review / issue review / repository audit service.
> This sprint builds the **engine only**. No target repository, no downstream adapter, and no real-world corpus is in scope yet — those are separate follow-on efforts.
>
> **Consumption contract:** a repository integrates by adding a single step to its own `ci.yaml`. This project does no per-repository work and ships no repo-specific configuration — only a generic, configuration-driven action.

## 1. Objective

Build a single, general-purpose tool that scans a repository — a PR diff, an issue's delivered work, a sprint's commit range, or the whole codebase — and produces durable, severity-classified findings. It ships as a CLI (`revu`) plus a reusable GitHub Action that a consuming repository invokes from its own `ci.yaml` with one step.

The tool is **one deployable** exposing multiple **profiles** over a shared pipeline. Review modes differ only in scope, inputs, evidence providers, and output template — not in architecture.

The engine is repository-agnostic: every notion of "which repo", "where findings live", and "which formats apply" is configuration passed at call time. There is no repository-specific code in this project, only compatibility with how consumers call it.

## 2. Locked Decisions

| Decision | Choice |
|---|---|
| Repo / stack | New standalone repo, **Go**, single `revu` binary |
| First provider | **OpenAI-compatible HTTP**, configurable `base_url` / `model` / key; registry leaves room for vendor-specific providers later |
| Finding archive | Dedicated **`review-artifacts` bot branch**; JSON + MD per run; published summary links to a commit-SHA permalink |
| Issue-close gate | Default **comment-only**; `auto_reopen` and `block_merge` are per-repo config |
| Persistence | **Tracking on by default** (fingerprint lifecycle); `mode: stateless` for single-run reports |
| Primary runtime | Reusable GitHub Action invoked by **one step** in a consumer's `ci.yaml`; CLI remains fully usable locally and provider-agnostically |

## 3. Profiles

| Profile | Trigger / input | Scope | Primary question | Output |
|---|---|---|---|---|
| **PR review** | PR number | `base..head` diff | Is this diff correct/safe/complete for its linked issue? | Review Summary (blocking/non-blocking) |
| **Issue review** | Issue number / `Closes #n` | acceptance criteria + linked commits/files | Was the issue actually implemented and closed? | Criteria checklist + gaps |
| **Sprint review** | commit range / milestone | all commits in range | Did the sprint deliver coherently; what is partial? | Findings + delivery table |
| **Repo audit** | branch / phase boundary | whole repo | Codebase health across audit areas | Audit findings + summary |
| **Refactor / impact** | commit range | touched subsystems + dependents | Did the refactor break or orphan things? | Regression + orphan findings |

## 4. Architecture

Single deployable; profiles are configuration over one shared pipeline.

### 4.1 Pipeline

1. **Ingest** — git (local ref fetch, `base..head` ranges) + GitHub REST; linked issues/ADRs; commit → slice mapping.
2. **Scope resolution** — profile + input → file set / symbol set / acceptance-criteria set.
3. **Context building** — dependency graph, ownership/boundaries, related docs.
4. **Static analyzers** — build/vet/format/test, architecture tests, grep-based probes (pluggable per ecosystem).
5. **Semantic review** — LLM passes per module against rubrics (correctness, concurrency, error handling, security, tests, missing implementation).
6. **Specialized detectors** — see §6.
7. **Normalization** — dedup, severity/classification assignment, evidence (`file:line` + snippet), drop unproven findings.
8. **Gating & output** — apply P0/P1 block policy; render per-profile template; publish to GitHub / archive.

### 4.2 Repository layout

```
cmd/revu/main.go
internal/
  config/        # revu.yaml + env/secrets, profile definitions
  vcs/           # git (local) + github (REST) ingest
  provider/      # Provider interface + openai-compatible impl + registry + fake
  scope/         # profile -> file/symbol/acceptance-criteria sets
  analyzers/     # static runners: go, python, ts (pluggable)
  reviewers/     # semantic rubric passes
  findings/      # Finding schema, severity/classification, fingerprint, dedup
  store/         # tracking store (SQLite/JSON) + stateless mode
  report/        # renderers: code-review + audit templates
  github/        # PR comment, issue create, archive-branch publish
  profiles/      # pr | issue | sprint | audit | impact wiring
profiles/        # built-in default profile definitions
testdata/        # synthetic seeded-defect fixtures
action.yml       # reusable GitHub Action (the thing consumers call)
.github/workflows/  # reusable workflow + this project's own CI
```

### 4.3 Consumption model

A consuming repository adds exactly one step to its own `ci.yaml`. Nothing else is added to that repository, and no changes are made in this project for it:

```yaml
- uses: <owner>/review-engine@v1
  with:
    profile: pr            # pr | issue | sprint | audit | impact
    provider: openai-compatible
    model: <model-id>
  env:
    REVIEW_PROVIDER_API_KEY: ${{ secrets.REVIEW_PROVIDER_API_KEY }}
```

Everything repository-specific — paths, ignores, severity gates, archive location, format adapters — is passed as action inputs or read from an optional `revu.yaml` in the consumer. Compatibility means the action works for any repository with configuration alone.

### 4.4 Finding record

Each finding carries enough context to survive the session and be actioned later:

```jsonc
{
  "id": "<fingerprint>",
  "title": "Timeout error is swallowed by the process runner",
  "classification": "Bug",                 // Bug | Technical Debt | Refactor | Security | Test | Documentation
  "severity": "P1",                        // P0 | P1 | P2 | P3
  "evidence": { "file": "internal/runner/exec.go", "line": 47, "snippet": "..." },
  "impact": "...",
  "recommendation": "...",
  "scope": "internal/runner",
  "suggested_phase": "...",
  "profile": "pr",
  "detector": "semantic.correctness",      // or a specialized detector id
  "status": "new",                         // new | ongoing | resolved | regressed
  "first_seen": "<run_id>",
  "last_seen": "<run_id>",
  "run_id": "..."
}
```

**Fingerprint:** `sha256(normalized_file_path, rule_or_detector_id, code_anchor)` — deliberately **not** the LLM prose, so rewording does not churn lifecycle state.

## 5. Stages

### Stage 0 — Foundation
**Slices (one commit each):**
- Repo scaffold (`go.mod`, layout, license) + CI skeleton (`gofmt`, `go vet`, `go test`).
- `findings` schema + severity/classification enums + fingerprint function.
- `config` loader (`revu.yaml`, env/secrets, profile definitions).
- `provider` interface + OpenAI-compatible impl + hermetic fake + registry.
- CLI skeleton (`revu run | plan | report`) with `--dry-run`.
- ADRs: provider abstraction, findings schema, security model, archive strategy.

**Acceptance:** `revu run --profile pr --dry-run` executes against a synthetic local git repo; the fake provider returns schema-valid findings; output is deterministic (temperature 0, pinned model, sorted).

### Stage 1 — PR review MVP (local CLI, end-to-end)
**Slices:**
- git/GitHub ingest (PR ref fetch, `base..head`).
- `scope` resolver = diff.
- Go static analyzers (`build`, `vet`, `gofmt -l`, `test`, architecture-test hook).
- Context builder.
- Semantic rubric pass.
- Normalizer / dedup.
- Markdown renderer in the code-review template.
- JSON archive + stateless mode.

**Acceptance:** against synthetic repos with deliberately seeded defects, the PR profile reports the seeded defects at the expected severities within the token budget, and reports none on a clean diff.

### Stage 2 — Persistence & tracking
**Slices:** SQLite store + JSON fallback; lifecycle transitions (new/ongoing/resolved/regressed); cross-run delta section; `mode: stateless`.

**Acceptance:** two consecutive runs on the same commit produce zero `new` findings; resolving a seeded defect marks it `resolved`.

### Stage 3 — Reusable Action & publishing
**Slices:** reusable `action.yml` + workflow; two-job fork-safe split; `fetch-depth: 0`; `review-artifacts` branch publish + permalink; PR summary comment; permission model; LLM-response cache via Actions cache; all repository-specific behavior expressed as action inputs / `revu.yaml`.

**Acceptance:** a consumer repository adds a single `uses:` step to its `ci.yaml` (no other changes) and gets one summary comment with a working permalink; reruns are no-ops; a fork PR completes without leaking secrets; two different repositories can call the same action with only input differences.

### Stage 4 — Issue review & closure gating
**Slices:** acceptance-criteria extraction from issue/PR bodies; map criteria → commits/files/tests; criteria-checklist finding type; `issues: closed` post-hoc audit; pre-merge `Closes #n` required check.

**Acceptance:** an issue closed without satisfying a criterion is flagged; a PR whose `Closes` target fails the gate is blocked **when `block_merge` is enabled**; default remains comment-only.

### Stage 5 — Audit & sprint profiles + specialized detectors
**Slices:** whole-repo audit profile; sprint-range profile; detectors (§6); ecosystem plugins; `.review-ignore` allowlist.

**Acceptance:** every detector has a seeded regression fixture and reports it at the expected severity; no detector fires on the clean fixture.

### Stage 6 — Generalization & hardening
**Slices:** multi-repo config; ecosystem auto-detection; adapter-pack extension point (formats, analyzers); cost/rate/token controls; prompt-injection hardening; docs; release/versioning.

**Acceptance:** the engine runs on a repository with no bespoke configuration beyond `revu.yaml`; model output attempting embedded instructions cannot alter the posted comment or gates.

## 6. Specialized Detectors (high value)

These are non-generic checks that a plain linter will not catch.

| Detector | Catches |
|---|---|
| **Orphan / wiring** | Exported constructors/APIs with only test callers and no composition root |
| **Test-masking** | New production code exercised only through fakes/stubs; real adapter path untested |
| **Contract drift** | Code vs schemas/ADRs: documented-but-unimplemented claims, missing error codes, param-name mismatch |
| **Stale reference** | Dangling imports/paths/strings after removals, in code **and** current-state docs |
| **Fail-open** | Nil-guard patterns where a missing dependency silently disables a check |

## 7. Security Model

- **Never** check out untrusted PR code in a secret-bearing job. Two-job split: Job A (`pull_request`, no secrets) collects context + runs analyzers; Job B (`workflow_run` / `pull_request_target` without PR checkout) calls the provider and publishes.
- **Model output is untrusted** — schema-validate before any GitHub write; sanitize before posting; never interpret model text as control flow.
- **Repository text is untrusted** — treat as data, not instructions; guard against prompt injection from source comments/docs/filenames.
- **Archive writes** go only to the `review-artifacts` bot branch; never to the PR branch.
- **Secrets** only via env/Actions secrets; never logged.

## 8. Cost & Reproducibility

- Response cache keyed by content hash.
- Temperature 0; pinned model version; stable sort/dedup.
- Diff-size chunking and per-run token/time budgets.
- Rate-limit backoff for the provider and GitHub API.

## 9. Testing Strategy

- **Synthetic corpus:** small git repos with deliberately seeded defects, one fixture per detector and per severity.
- **Eval harness:** measures recall/precision against the seeded catalog.
- **Unit tests:** fingerprint, dedup, scope resolution, config parsing.
- **Snapshot tests:** report rendering in both templates.
- **Hermetic fake provider** for CI (no network).

## 10. Non-Goals

- No automatic code fixes.
- No replacement for human review.
- No writes to PR branches.
- No multi-provider routing in v1 (interface only).
- No downstream/target-repository adapters in this sprint — extension points only.
- **No repository-specific work.** Integration is a single `uses:` step in the consumer's `ci.yaml`; everything else is configuration. Compatibility, not customization.

## 11. Cross-Cutting Definition of Done

- `gofmt` / `go vet` / `go test ./...` clean.
- Every new public interface has tests validating meaningful behavior.
- Every specialized detector has a seeded regression fixture.
- Docs updated (README, profile reference, config reference).
- No temporary debug code.

## 12. Immediate Next Steps (Stage 0)

1. Create repo layout + `go.mod`.
2. Add CI (`gofmt`/`go vet`/`go test`).
3. Define `Finding` schema + enums + fingerprint.
4. Scaffold `config`, `provider` (interface, OpenAI-compatible, fake).
5. Land CLI skeleton with `--dry-run`.
6. Commit the first synthetic seeded-defect fixture; begin Stage 1.
