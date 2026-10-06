# ADR-0006: Optional SQLite tracking store

## Status

Accepted

## Context

Tracking state (findings and their lifecycle) is persisted between runs. The
default must be zero-dependency and human-inspectable; larger repositories may
want indexed, concurrent access.

## Decision

Keep the **JSON store as the default** and add an **opt-in SQLite backend**,
disabled by default and enabled with:

```yaml
store:
  sqlite: true
  path: ""      # default: <state-dir>/review.db
```

or `REVIEW_STORE_SQLITE=true`. Both backends implement the same `store.Store`
interface (`Load`/`Save` of the tracked set) and hold the identical `Finding`
records, so switching is transparent. The SQLite backend uses the pure-Go
`modernc.org/sqlite` driver, so no cgo toolchain is required. Opening the store
creates the parent directory when needed, and `Save` replaces the tracked set in
a transaction.

## Consequences

- Default remains dependency-light and diffable as JSON.
- Users who opt in get a single indexed file and atomic replacement.
- The driver is a large transitive dependency; it is pulled in regardless of the
  backend because it is compiled into the binary. Acceptable for a CLI.
- The schema is intentionally minimal (`findings(id, run_id, data)`); a future
  migration can add columns without changing the interface.
