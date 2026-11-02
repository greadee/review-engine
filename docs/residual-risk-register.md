# Residual-risk register

This register records what the engine **cannot prove**, even when its tests pass.
It is written for users and reviewers, not as an internal TODO list. Every open
risk has an accountable owner and a target release for review or closure.

**Status reviewed for:** `v1.3.0 Narwhal`
**Owner convention:** `greadee (maintainer)` is the accountable owner for every
item in this register.
**Target release convention:** the earliest release in which the risk will be
re-reviewed or reduced. `accepted` means the limitation is inherent and is
mitigated rather than removed.

| ID | Residual risk | Why it remains | Current mitigation | Owner | Target release |
|---|---|---|---|---|---|
| R-01 | Issue evidence is reviewable, not proof. | The GitHub API can prove that a PR/commit exists, is merged, and touched the relevant area; it cannot prove that the change semantically satisfies the criterion. | Resolve references, require merged/closed state, check area overlap, record rejection reasons, and keep human closure authority. | greadee (maintainer) | accepted in `1.3.0` |
| R-02 | Model findings are probabilistic and can vary between runs. | The pinned model is not deterministic even at temperature 0; a repeat run may produce different findings. | Pin the model and settings, publish measured results and variance, and fail closed to incomplete coverage on provider failures. | greadee (maintainer) | `1.5.0` |
| R-03 | Real-model accuracy is a snapshot from a small synthetic corpus. | The corpus cannot represent every repository shape, language, or defect class. | Publish methodology, known gaps, precision/recall, and the deterministic oracle gate alongside the pinned-model report. | greadee (maintainer) | `1.4.0` |
| R-04 | Prompt-injection resistance is not exhaustively provable. | The adversarial suite covers known attack classes, but a future model or novel injection can still produce false findings or miss a defect. | Mark repository text as untrusted, use per-request nonce boundaries, sanitize all comments, fail incomplete on bad output, and keep the adversarial suite. | greadee (maintainer) | `1.5.0` |
| R-05 | Static analyzer coverage is incomplete. | Only Go, Python, and TypeScript analyzers are built in; missing tools or unsupported ecosystems produce gaps. | Report analyzer coverage gaps explicitly and never convert a gap into an approval. | greadee (maintainer) | `1.4.0` |
| R-06 | Analyzer subprocesses can reach the network or download toolchains. | Repository build tooling runs in the collect job; scrubbed environments and timeouts reduce but do not eliminate network access. | Use the two-phase flow so analyzers run without provider/GitHub secrets, and document the local trust boundary. | greadee (maintainer) | `1.5.0` |
| R-07 | Whole-repo and range scans can hit file or byte ceilings. | Bounded resources are required; a very large repository cannot be fully reviewed in one run. | Cap collection at bundle limits, count skipped/truncated files, and return an explicit incomplete result. | greadee (maintainer) | accepted in `1.3.0`; re-review `1.5.0` |
| R-08 | GitHub Enterprise permalinks are not supported. | Archive links assume `api.github.com` and `github.com`. | Document the limitation; use the repository URL directly until GHE support lands. | greadee (maintainer) | `1.4.0` |
| R-09 | CI lifecycle state is best-effort. | Actions caches can be evicted; a cold cache starts a fresh lifecycle. | Scope cache keys per repository/PR/profile and never let one PR corrupt another PR's state. | greadee (maintainer) | `1.5.0` |
| R-10 | Provider outages, rate limits, and cost variance remain possible. | The engine depends on an external model provider. | Bounded retries, retry-exhaustion reporting, incomplete coverage on failure, and published token/cost estimates. | greadee (maintainer) | `1.5.0` |
| R-11 | The supported environment is narrower than the advertised problem space. | Linux GitHub Actions and the documented Go toolchain are tested; Windows, macOS, non-GitHub CI, and unusual repository layouts are not certified. | Document supported environments and keep the CLI usable offline; collect reproducible reports for unsupported environments. | greadee (maintainer) | `1.4.0` |
| R-12 | Sanitization prevents markup injection but not misleading prose. | Escaping stops HTML, mentions, images, and backtick breakouts; it cannot make model-written findings correct. | Use the shared sanitizer on every comment path and require human review of findings. | greadee (maintainer) | accepted in `1.3.0` |

## Review cadence

- Review this register at every release and after any security incident.
- Update the target release when work is deferred.
- Do not remove an item merely because it is inconvenient; change it to a
  documented accepted risk with a mitigation and owner.
- Known user-facing constraints are also listed in
  [`limitations.md`](limitations.md).
