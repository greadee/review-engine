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

- **Criterion evidence is author-supplied.** In `internal/issues`, a criterion is
  "verified" when it cites a repository path that exists, a `#PR`/commit
  reference, or an explicit `(verified)`/approval marker. These references are
  reviewable but not independently validated, and the reference pattern
  (`[0-9a-f]{7,40}`) can match a hex-looking word. Treat issue verification as a
  prompt for a human, not proof.

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
