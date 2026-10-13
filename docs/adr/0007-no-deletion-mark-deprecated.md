# ADR-0007: Records are never deleted; mark deprecated with strikethrough

## Status

Accepted

## Context

Documentation, contracts, and decision records point at code paths that change.
When a path is removed or renamed, the reference becomes stale. The instinct is to
delete the stale line — but for ADRs, phase summaries, and update snapshots the
text is a historical record of what was true at the time. Deleting it destroys the
audit trail and rewrites history.

## Decision

**Never delete from a record.** When a section becomes superseded, obsolete, or
not applicable, mark it in place in Markdown with a **strikethrough** (`~~...~~`)
and, where useful, a short deprecation note:

```markdown
~~`contracts/schemas/v1/trace.schema.json` — superseded by `schemas/v2` in the
ten-issue sprint (Issue 1 / A4).~~
```

The stale-reference detector honours this rule: content inside a strikethrough
span is treated as a deliberate historical record and is **not** reported. Its
recommendation is to mark the reference deprecated in place, never to remove it.

## Consequences

- History is preserved; readers can see what was true and what superseded it.
- The stale-reference finding is resolved by striking the reference, not editing
  it away.
- Automated checks must ignore strikethrough content, which the detector does.
