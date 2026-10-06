# ADR-0003: Security model

## Status

Accepted

## Context

The engine runs against untrusted input: a pull request's code, its comments, and
its file names. It also holds a model API key and a GitHub token with write access.

## Decision

1. **Never check out untrusted PR code in a secret-bearing job.** For forks, split
   into two jobs: one without secrets that checks out the PR and produces context
   artifacts, and one with secrets that runs the model and publishes, without
   checking out untrusted code.
2. **Model output is untrusted.** It is parsed into the strict finding schema and
   validated before any GitHub write. Model text is never interpreted as control
   flow and is not posted verbatim without rendering.
3. **Repository text is untrusted.** Source, comments, docs, and file names are data,
   not instructions; prompts must not allow repository content to change engine
   behavior or gates.
4. **Archives are written only to a dedicated bot branch**, never the default or PR
   branch.
5. **Secrets are read from the environment** and never logged.

## Consequences

- The default Action is safe for same-repository PRs; fork safety requires the
  documented two-job pattern.
- Publishing needs `contents: write`; commenting needs `pull-requests: write`.
- Prompt-injection resistance is a first-class concern for the semantic pass.
