# Known limitations

These are minor, known constraints of the current engine. They are recorded here
rather than tracked as work items; the items that need fixing are filed as
issues.

- **GitHub Enterprise permalinks.** `blobURL` (`internal/engine/publish.go`)
  rewrites `https://api.github.com` to `https://github.com`. On GitHub
  Enterprise the archive permalink will be wrong; GHE is not supported yet.

- **Strict bundle schema.** `bundle.Load` uses `DisallowUnknownFields`, which is
  intentional for an untrusted artifact but means the `collect` and `finalize`
  revisions must match. Pin both to the same commit when using the two-phase
  flow (the examples already do).

- **Criterion evidence remains reviewable, not proof.** In CI, the issue
  profile resolves `#PR`/issue/commit references through the GitHub API, checks
  that PRs are merged and issues are closed, and requires the referenced work to
  touch the issue's areas. A repository path is still checked by existence, and
  a hex-looking word is rejected unless it resolves as a commit. The API cannot
  prove that a change semantically satisfies the criterion, so a human reviewer
  remains responsible for the final judgment.

- **`issues.EvaluateWith`** is currently referenced only by tests (the engine's
  own orphan detector flags it, P2). It is retained as public API.

- **Pinned-SHA ancestor check.** The reference test (`internal/refs`) requires a
  pinned SHA to be an ancestor of the checkout. It can fail on a pull request
  branched before the pinned revision; CI runs on `main` and freshly branched
  PRs, where it holds.

- **Analyzers may use the network.** The static analyzers run the repository's
  build tooling (`go build`/`go vet`, `ruff`, `tsc`) during collect, which can
  fetch dependencies or a `toolchain` declared in `go.mod`. For untrusted pull
  requests use the two-phase flow so this runs in the secret-free job.

- **Trusted collect policy.** `collect` reads `revu.yaml` from the base revision,
  not the pull-request checkout. A PR cannot change ignore rules, static/semantic
  toggles, or review limits. Consumers that relied on PR-local policy must move
  it to the base branch or a trusted global/workflow configuration.

- **PR scope verification.** PR and impact finalization requires the GitHub API,
  `--repository`, and `--pr` so it can verify the bundle against the pull
  request's changed files. A local finalize without those values is rejected;
  `--dry-run` remains offline.

- **Archive branch guard.** Remote archive writes refuse the repository default
  branch and the pull request head branch. `archive.branch` must name a dedicated
  bot branch.

- **Existing duplicate comments.** The 1.2 comment contract updates the first
  marker comment in place. It does not delete duplicate marker comments created
  by earlier append-per-run releases; clean those up once manually if needed.

- **CI tracking cache is best-effort.** The example workflows cache
  `.review-state` per pull request and profile, scoped under
  `.review-state/pr-<number>/<profile>`. A
  cold or evicted cache starts a fresh lifecycle; it does not corrupt an existing
  PR's findings.
