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
[`examples/issue-review.yml`](examples/issue-review.yml). See
[`docs/consumer-install.md`](docs/consumer-install.md) for the minimal install
steps and the exact permissions each job needs.

### Versioning and pinning

`greadee/review-engine@v1` is a maintained major tag that always points at the
latest reviewed v1 release. For maximum reproducibility, pin to a reviewed full
commit SHA instead:

```yaml
- uses: greadee/review-engine@<40-char-commit-sha>
```

The examples and the reusable workflow are pinned to full commit SHAs. `@main`
(the moving branch) must not be used for the action in a job that holds a
provider key or a write-capable token. CI verifies that every documented
reference resolves and is pinned, and that the `v1` tag is published.

## Issue review

The `issue` profile extracts acceptance criteria (checkboxes, or list items under
an acceptance/criteria heading) from an issue body and reports the ones that are
**not independently verified**.

A checkbox is a claim, not evidence. A criterion counts as satisfied only when it
cites independently checkable evidence:

- a repository path that exists (a linked test or reviewed change), e.g.
  `` `internal/auth/login_test.go` ``;
- a PR/issue/commit reference (`#123` or a commit SHA); or
- an explicit `(verified)` / `(approved)` reviewer marker, or the issue carrying
  the configured `issueReview.verifiedLabel`.

Consequences:

- A checked box with no evidence is reported as **checked but unverified**, and
  never treated as delivered work.
- A delivered criterion left unticked is still satisfied if it carries evidence.
- On an **open** issue, unverified criteria are P3; on a **closed** issue,
  unchecked criteria are P1 and checked-but-unverified criteria are P2.

Closure gating is comment-only by default. Additional behavior is opt-in via
`revu.yaml` (`issueReview.autoClose` / `autoReopen` / `blockMerge` /
`verifiedLabel`) or flags:

- `--close` closes an open issue only when **every** criterion is independently
  verified (or explicitly approved); an issue with no criteria is never
  auto-closed.
- `--reopen` reopens a closed issue that still has unverified criteria.
- `--block` fails the step when criteria lack verified evidence or are absent
  (use as a required check). Checklist state alone never passes the check.

Criteria without verified evidence always leave the issue open.

> Fork PRs cannot access secrets. For untrusted forks — and to keep a
> same-repository PR from modifying `revu.yaml` — run the collect phase without
> secrets and finalize in a separate secret-bearing job. See
> [`docs/adr/0005-two-phase-review.md`](docs/adr/0005-two-phase-review.md) and
> [`examples/fork-safe/`](examples/fork-safe/).

## Provider endpoint trust

The provider sends `REVIEW_PROVIDER_API_KEY` as a Bearer token to
`provider.baseUrl`, which is configurable in `revu.yaml`. A same-repository pull
request can edit that file, so the secret-bearing job must not trust it. The
engine therefore refuses to attach the key to any endpoint that trusted,
workflow-controlled configuration has not allowlisted:

- the built-in default `https://api.openai.com/v1` is always allowed;
- additional endpoints must be listed in the `REVIEW_PROVIDER_ALLOWED_BASE_URLS`
  environment variable (the `allowed-base-urls` action input) or in the **global**
  config (`provider.allowedBaseUrls`). The repository's `revu.yaml` is ignored
  for this decision.

Pin the endpoint from the workflow too, so a PR cannot change it at all:

```yaml
- uses: greadee/review-engine@v1
  with:
    base-url: https://api.openai.com/v1
    allowed-base-urls: https://api.openai.com/v1
```


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

### Local (offline) mode

`revu run --local` writes findings to disk and never touches GitHub — no comment,
no archive branch, no writes of any kind. It is project-scoped and mirrors the
remote storage hierarchy:

```
<local.dir>/<project>/           # e.g. .review-engine/aa
  runs/<runID>.json              # findings (same layout as the archive branch)
  runs/<runID>.md
  state.json                     # tracking state
```

`<project>` is the repository name (so a run on `greadee/aa` appends to
`…/aa/`). Set the root with `local.dir` / `--local-dir` (or `REVIEW_LOCAL_DIR`);
`--quiet` suppresses the rendered summary. See `revu.example.yaml`.

### Persistence policy

`persist.mode` (or `--persist`) controls when findings are written to **any**
store — local, remote archive, and tracking. Notification (the PR/issue comment)
is independent and still runs when enabled.

| Mode | Writes when |
|---|---|
| `always` (default) | always |
| `on-findings` | at least one finding exists |
| `on-risk` | a finding is at or above `persist.risk` (P0–P3; `--risk`) |
| `never` | never — no local, archive, or tracking write; comment/notify only |

```bash
revu run --persist on-risk --risk P2      # only store when a finding is P2+
revu run --persist on-findings            # only store when findings exist
revu run --persist never                  # compute + notify, store nothing
```

### Review coverage

The semantic pass is bounded (`review.maxFiles`, `review.maxFileBytes`) and runs
in batches, so a large PR can never be silently approved after only part of it
was reviewed. Every run records what was **selected**, **analyzed**, **skipped**
(past `maxFiles`), **truncated** (past `maxFileBytes`), and any **failed
batches**; the summary comment, the archive, and the JSON all carry these
numbers.

- A result whose coverage is incomplete is never reported as `Approve`; the
  verdict becomes `Incomplete`.
- The required check fails when coverage is incomplete unless `--no-block` is
  set.
- `review.coverageThreshold` (action input `coverage-threshold`, env
  `REVIEW_COVERAGE_THRESHOLD`) sets the minimum analyzed fraction, `0..1`
  (default `1.0`). Lower it to accept partial coverage; raise `maxFiles` /
  `maxFileBytes` to actually cover more.

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
| `detector.stale-reference` | Documentation pointing at repository paths that no longer exist (content already marked deprecated with `~~strikethrough~~` is ignored — records are never deleted) |
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

### Corpus & accuracy

`testdata/corpus/` is a seeded PR corpus (correctness, security, reliability,
test-gap, clean, and prompt-injection fixtures) with agreed release thresholds.
Run it hermetically with the deterministic oracle provider:

```bash
revu eval --corpus testdata/corpus --check
```

See [`docs/corpus.md`](docs/corpus.md) for the fixtures, metrics, thresholds, and
how to run the optional pinned-model evaluation.

Known constraints are recorded in [`docs/limitations.md`](docs/limitations.md).

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
