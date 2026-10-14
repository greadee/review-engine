# Consumer installation

`revu` integrates with a consuming repository through configuration and a single
action step per job. There is no per-repository code in this project.

## Secret

Create one repository secret:

- `REVIEW_PROVIDER_API_KEY` — the provider API key. It is read from the
  environment and never logged or interpolated into a shell script.

## Permissions

Grant the least privilege each job needs.

| Job | Permissions | Why |
|---|---|---|
| collect | `contents: read` | read the PR diff; **no** provider key |
| finalize | `contents: write` | archive findings to the `review-artifacts` branch |
| finalize | `pull-requests: write` | upsert the summary comment |
| finalize | `issues: write` | issue profile only (comment/close/reopen) |

The collect job must never receive `REVIEW_PROVIDER_API_KEY`.

## Recommended: two-phase (fork-safe) integration

This is the recommended setup for any untrusted pull request — a fork or a
same-repository PR that can edit `revu.yaml`.

### 1. Collect (no secrets)

Copy [`examples/fork-safe/collect.yml`](../examples/fork-safe/collect.yml). It
runs on `pull_request`, has `contents: read` only, checks out the PR, produces a
bundle, and uploads it as an artifact. The pinned action revision is referenced
by full commit SHA.

### 2. Finalize (secrets, no checkout)

Copy [`examples/fork-safe/finalize.yml`](../examples/fork-safe/finalize.yml). It
is triggered by `workflow_run`, downloads the artifact, and runs `phase:
finalize`. It never checks out the pull request, and it binds the artifact to the
trusted event:

```yaml
expect-repository: ${{ github.event.workflow_run.repository.full_name }}
expect-pr: ${{ github.event.workflow_run.pull_requests[0].number }}
expect-head: ${{ github.event.workflow_run.head_sha }}
expect-profile: pr
base-url: https://api.openai.com/v1
allowed-base-urls: https://api.openai.com/v1
```

## Single-step (trusted changes only)

If PRs cannot modify `revu.yaml` (for example, a protected configuration reviewed
separately), a single step is enough. Copy
[`examples/review.yml`](../examples/review.yml). Pin `base-url` and
`allowed-base-urls` from the workflow so a PR cannot redirect the provider key,
and pin the action to a full commit SHA.

## What the harness proves

`internal/integration` is a repeatable harness that builds a consumer repository
and drives the two-phase flow against an in-memory GitHub API. It verifies:

- collect runs with no provider/secret and read-only scope;
- finalize never checks out the PR and binds the bundle to the expected
  repository, PR, and head SHA;
- a validated blocking finding fails the check, and a fully covered clean PR
  passes;
- publish produces an immutable commit-SHA permalink;
- reruns upsert a single comment (no duplicates);
- the workflows keep the secret-bearing job out of untrusted checkout.
