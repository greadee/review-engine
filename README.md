# Review Engine (`revu`)

General-purpose code review, PR review, issue review, and repository audit service.

`revu` scans a repository — a PR diff, an issue's delivered work, a sprint's commit
range, or the whole codebase — and produces durable, severity-classified findings. It
is one engine with pluggable **profiles** over a shared pipeline. It ships as a CLI
and as a reusable GitHub Action that a repository calls with **a single step** in its
own `ci.yaml`.

- **No per-repository work.** A consumer adds one `uses:` step; everything else is
  configuration. This project does no repository-specific customization.
- **Provider-agnostic.** One `openai-compatible` provider today; the interface and
  registry leave room for vendor-specific providers later.
- **Durable findings.** Results are archived to a bot branch and linked from the PR;
  findings are tracked across runs by a stable fingerprint.

## Quick start (GitHub Actions)

Add one step to your workflow:

```yaml
permissions:
  contents: write        # archive findings to the bot branch
  pull-requests: write   # post the summary comment
  issues: write

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: greadee/review-engine@v1
        with:
          profile: pr
          model: gpt-4o-mini
          api-key: ${{ secrets.REVIEW_PROVIDER_API_KEY }}
          base: origin/${{ github.base_ref }}
          head: ${{ github.sha }}
          pr: ${{ github.event.pull_request.number }}
```

The action builds the engine, runs the selected profile, archives findings to the
`review-artifacts` branch, and posts a summary comment on the PR. More examples:
[`examples/review.yml`](examples/review.yml) and
[`examples/issue-review.yml`](examples/issue-review.yml).

## Issue review

The `issue` profile extracts acceptance criteria (checkboxes, or list items under
an acceptance/criteria heading) from an issue body and reports the ones that are
not satisfied.

- On an **open** issue, unmet criteria are P3 (work in progress).
- On a **closed** issue, unmet criteria are P1 (the closure is unsupported).

Closure gating is comment-only by default. `--block` fails the step when criteria
are unmet (use as a required check), and `--reopen` reopens a closed issue with
unmet criteria. Both are opt-in via `revu.yaml` / action inputs.

> Fork PRs cannot access secrets. For untrusted forks, run the collect phase
> without secrets and finalize in a separate secret-bearing job. See
> [`docs/adr/0005-two-phase-review.md`](docs/adr/0005-two-phase-review.md) and
> [`examples/fork-safe/`](examples/fork-safe/).

## Fork-safe CI

A review runs in two phases so untrusted code never shares a job with the API key:

```bash
# Phase 1 — no secrets, safe against untrusted code
revu collect --profile pr --base origin/main --head HEAD --out bundle.json

# Phase 2 — secrets, never checks out the PR
revu finalize --bundle bundle.json --comment --publish
```

`revu run` is simply collect + finalize in one process. The reusable workflow
[`.github/workflows/review.yml`](.github/workflows/review.yml) runs both jobs;
for forks, bridge them with `workflow_run` as shown in `examples/fork-safe/`.

## Local CLI

```bash
go build -o revu ./cmd/revu

# Review a diff between two revisions
./revu run --profile pr --base origin/main --head HEAD --repo-dir .

# Plan (list the files a profile would review) without analyzing
./revu plan --profile pr --base origin/main --head HEAD

# Render a findings JSON file later
./revu report --in findings.json --audit

# Review an issue against its acceptance criteria (comment-only by default)
./revu issue --issue 42 --repository owner/name --comment

# Inspect the effective configuration and the environment
./revu config
./revu doctor
```

Useful flags: `--dry-run` (fake provider, no GitHub writes), `--static=false`,
`--provider`, `--model`, `--repository owner/name`, `--pr N`, `--publish`,
`--comment`, `--state-dir`.

## Profiles

| Profile | Input | Scope | Output |
|---|---|---|---|
| `pr` | PR number | `base..head` diff | Review summary |
| `issue` | issue / `Closes #n` | acceptance criteria + linked work | Criteria checklist |
| `sprint` | commit range | all commits in range | Audit-style summary |
| `audit` | branch | whole repo | Audit findings |
| `impact` | commit range | touched subsystems | Regression / orphan findings |

## Configuration

See [`revu.example.yaml`](revu.example.yaml). All fields are optional. Environment
variables (`REVIEW_PROFILE`, `REVIEW_MODEL`, `REVIEW_PROVIDER_API_KEY`, …) override
the file, which is how the Action passes its inputs.

### Multi-repo conveniences

- **Config layering.** Built-in defaults are overlaid by an optional global config
  (`REVIEW_GLOBAL_CONFIG`, else `<user-config-dir>/revu/config.yaml`), then the
  repository's `revu.yaml`, then environment overrides. One global config can set
  defaults shared by many repositories.
- **Per-profile overrides.** A `profiles:` map lets a single file configure every
  review type (rubric, ignore globs, block threshold, static/semantic, limits).
- **CI auto-detection.** Base, head, repository, and PR number are inferred from
  GitHub Actions (`GITHUB_*`) when not passed explicitly, so the same step works
  in any repository. `revu doctor` shows what was detected.
- **Backend selection.** `revu config` prints the effective configuration; the
  tracking backend is chosen with `store.sqlite` (see below).

### Tracking store

Tracking is on by default and uses the JSON store at `--state-dir`
(`.review-state`). Set `store.sqlite: true` (or `REVIEW_STORE_SQLITE=true`) to use
the opt-in SQLite backend, which is pure-Go (no cgo) and stores at
`<state-dir>/review.db` by default. Both backends implement the same interface and
hold identical findings. See [`docs/adr/0006-optional-sqlite-store.md`](docs/adr/0006-optional-sqlite-store.md).

## Detectors

Beyond generic static analysis and the model pass, the engine runs specialized
detectors that catch code which compiles and passes tests but is still wrong:

| Detector | Catches |
|---|---|
| `detector.orphan` | Exported functions with only test callers, or no callers — the "not wired into production" class |
| `detector.stale-reference` | Documentation pointing at repository paths that no longer exist |
| `detector.fail-open` | Nil-guarded checks where a missing dependency silently disables the guard |
| `detector.unfinished` | TODO/FIXME/XXX markers and not-implemented code paths |

They run as analyzers: for a PR they inspect changed files (with the whole repo as
reference context); for audit/sprint they scan the whole repository.

## Findings

Each finding carries a stable fingerprint, a classification, a severity (P0–P3),
evidence (`file:line` + snippet), impact, recommendation, and lifecycle status
(`new` / `ongoing` / `resolved` / `regressed`). See
`internal/findings` and `docs/adr/0002-findings-schema-and-fingerprint.md`.

## Architecture

```
cmd/revu              CLI
internal/
  config              revu.yaml + env overrides
  vcs                 git + GitHub REST
  bundle              collect/finalize artifact (fork-safe split)
  provider            Provider interface, openai-compatible, fake, registry
  scope               profile -> files
  analyzers           static runners (Go, Python, TypeScript)
  detectors           orphan/wiring, stale-reference, fail-open, unfinished
  reviewers           semantic rubric pass
  findings            schema, fingerprint, lifecycle
  store               JSON (default) or SQLite tracking store, + stateless
  ci                  CI environment detection
  report              review + audit templates
  engine              pipeline orchestration
  profiles            built-in profile defaults
```

Pipeline: ingest → scope → context → static analyzers → semantic review →
normalize/dedup → lifecycle → render → publish.

## Development

```bash
go build ./...
go vet ./...
go test ./...
gofmt -l .
```

## Status

Stages 0–5: the engine runs the `pr`, `audit`, and `issue` profiles end-to-end with
the Go static analyzer, the semantic pass, cross-run tracking with a delta section,
archiving, the orphan/wiring, stale-reference, fail-open, and unfinished detectors,
acceptance-criteria verification with closure gating, a fork-safe two-phase
(`collect`/`finalize`) flow with a reusable workflow, ecosystem detection for Go,
Python, and TypeScript analyzers, multi-repo conveniences (config layering,
per-profile overrides, CI auto-detection, `config`/`doctor`), and an optional
SQLite tracking store. See [`sprint-plan.md`](sprint-plan.md).

## License

MIT
