# Security policy

## Supported versions

Security fixes are applied to the latest stable `1.x` release. Older minor
lines may be asked to upgrade before a fix is provided.

## Reporting a vulnerability

Do not open a public issue for a security problem.

Use GitHub's private vulnerability reporting for this repository:

1. Open the repository's **Security** tab.
2. Select **Advisories**.
3. Choose **Report a vulnerability**.

Include the affected version or commit, a minimal reproduction, the impact, and
any relevant logs. Do not include provider keys, GitHub tokens, or other secrets.

## Scope

Reports are especially useful for:

- review-scope bypasses or false approvals;
- credential or token disclosure;
- prompt-injection paths that change the review gate;
- archive-branch or pull-request write issues;
- untrusted bundle or artifact handling;
- workflow-permission mistakes.

## Disclosure

Please allow time for a fix and a coordinated release before publishing details.
Credit will be given in the release notes unless you ask to remain anonymous.
