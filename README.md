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
`review-artifacts` branch, and posts a summary comment on the PR.

> Fork PRs cannot access secrets. For untrusted forks, run the static/context job
> without secrets and perform the model call and publishing in a separate
> `workflow_run` job. See `docs/adr/0003-security-model.md`.

## Local CLI

```bash
go build -o revu ./cmd/revu

# Review a diff between two revisions
./revu run --profile pr --base origin/main --head HEAD --repo-dir .

# Plan (list the files a profile would review) without analyzing
./revu plan --profile pr --base origin/main --head HEAD

# Render a findings JSON file later
./revu report --in findings.json --audit
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
  provider            Provider interface, openai-compatible, fake, registry
  scope               profile -> files
  analyzers           static runners (Go today)
  reviewers           semantic rubric pass
  findings            schema, fingerprint, lifecycle
  store               JSON tracking store + stateless
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

Stage 0–1 of the sprint plan: the engine runs the `pr` profile end-to-end with the
Go static analyzer and the semantic pass, with tracking and archiving. See
[`sprint-plan.md`](sprint-plan.md). The `issue`, `sprint`, `audit`, and `impact`
profiles are wired as configurations over the same pipeline; their dedicated
extractors and detectors are the next stages.

## License

MIT
