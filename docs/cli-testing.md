# CLI test coverage

`cmd/revu` contains the command wiring, exit-code handling, GitHub comment
paths, reconcile/issue close paths, and local review orchestration. Its tests
are table-driven where the behavior is command-shaped.

Run the suite with coverage:

```bash
go test ./cmd/revu -coverprofile=cli-cover.out
go tool cover -func=cli-cover.out
```

The documented floor is **60.0%** of `cmd/revu` statements. CI enforces it with
`scripts/check-cli-coverage.sh`; set `CLI_COVERAGE_FLOOR` to override the floor
for a one-off run.

## Covered command paths

- `run` with dry-run, persist `always`, persist `never`, and invalid flags
- `collect` including bundle output
- `finalize` with a dry-run bundle
- `issue` including comment and close API paths
- `reconcile` including its comment path
- `plan`, `report`, `init`, `config`, `file-issue`, and `eval`
- config and profile errors, missing bundles/issues, and exit codes `0`, `1`, `2`

`dispatch` returns the process exit code so tests can assert it without spawning
a subprocess. `GITHUB_API_URL` may point the GitHub client at a test server.
