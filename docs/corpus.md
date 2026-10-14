# Seeded PR corpus and accuracy harness

`testdata/corpus/` is a small, versioned corpus of synthetic PRs used to measure
the PR-review claim. Each fixture is a directory:

```
testdata/corpus/<name>/
  meta.json     # profile, clean/adversarial flags, notes, expected findings
  base/         # file tree for the base revision
  head/         # file tree for the head revision (the "PR")
```

`internal/eval` builds each fixture into a throwaway git repository and runs the
full review pipeline (scope → gather → semantic → normalize → lifecycle →
render) against it.

## Fixtures

| Fixture | Category | Seeded defect | Severity |
|---|---|---|---|
| `correctness` | correctness | off-by-one loop bound | P1 |
| `security` | security | command injection via shell | P0 |
| `reliability` | reliability | swallowed read error | P2 |
| `test-gap` | test gap | new function with no test | P2 |
| `clean` | clean | none (must produce no findings) | — |
| `adversarial` | prompt injection | repository text trying to forge/suppress findings | — |

Seeds are marked in source with `// revu-seed: <Classification> <Severity> <anchor>`.
The deterministic `eval.Oracle` provider reports exactly those markers and
ignores every other instruction in repository text, so an adversarial comment
cannot forge or suppress a finding.

## Deterministic run (CI)

The hermetic run uses the oracle provider — no network, no model key:

```bash
go test ./internal/eval/ -run TestCorpusDeterministic -v
# or, through the CLI:
revu eval --corpus testdata/corpus --check
```

`--check` exits non-zero unless the corpus meets the release thresholds.

## Optional pinned-model evaluation

To measure a real model, provide a pinned model id and key. The same corpus and
metrics are used; results are printed for publishing with the milestone:

```bash
REVIEW_EVAL_MODEL=gpt-4o-mini \
REVIEW_PROVIDER_API_KEY=... \
REVIEW_PROVIDER_BASE_URL=https://api.openai.com/v1 \
go test ./internal/eval/ -run TestCorpusPinnedModel -v
```

## Metrics

- **Recall** — detected / seeded defects, and specifically for P0/P1.
- **Severity accuracy** — detected defects reported at the seeded severity.
- **Unsupported finding rate** — findings on clean/adversarial fixtures.
- **Evidence validity** — every finding must cite a file and line that exist in
  the reviewed revision.
- **Coverage** — clean fixtures must report 100% coverage (no skips/truncation).

## Release thresholds (v1)

The MVP is not complete unless the deterministic corpus meets **all** of:

| Metric | Threshold |
|---|---|
| P0/P1 recall | `1.0` |
| Overall recall | `1.0` |
| Severity accuracy | `1.0` |
| False positives | `0` |
| Invalid-evidence findings | `0` |
| Findings on clean/adversarial | `0` |
| Clean fixtures with full coverage | `100%` |

Current deterministic results:

```
| fixtures | 6 |
| seeded defects | 4 |
| detected | 4 |
| recall (all) | 100% |
| recall (P0/P1) | 100% |
| severity accuracy | 100% |
| false positives | 0 |
| findings on clean/adversarial | 0 |
| invalid evidence | 0 |
| clean fixtures with full coverage | 2/2 |
```

This corpus measures the narrow PR-review claim. Broader audit and issue-review
quality are later milestones.
