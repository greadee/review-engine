# Release process

Releases are prepared from `main`, published from a reviewed commit, and moved
onto the `v1` major tag only after the release page is verified.

## Naming

- Tag and release title: `vX.Y.Z Animal Nickname`.
- Release purpose: the first paragraph of the release note.
- Example: `v1.2.0 Common Chimpanzee` with “This is the **Restore** release.”

Do not use the purpose as the animal nickname. See
[`versioning.md`](versioning.md).

## Preconditions

- The release note exists at `docs/releases/X.Y.Z.md`.
- `CHANGELOG.md` contains a `## [X.Y.Z]` section.
- The tree is clean and the release is prepared on `main`.
- `go test ./...`, `gofmt`, and `go vet` are clean.
- CI is green at the commit to be tagged.
- The target tag does not already exist.

## Release steps

1. Update `CHANGELOG.md`: replace `Unreleased` with the release date and purpose.
2. Finalize `docs/releases/X.Y.Z.md` with verified highlights, behavior changes,
   known issues, upgrade notes, and verification evidence.
3. Confirm the purpose is in the release note and the animal nickname is in the
   title.
4. Run the dry run:
   ```bash
   bash scripts/release.sh \
     --version vX.Y.Z \
     --title "vX.Y.Z Animal Nickname" \
     --notes docs/releases/X.Y.Z.md \
     --dry-run
   ```
5. Run the release:
   ```bash
   bash scripts/release.sh \
     --version vX.Y.Z \
     --title "vX.Y.Z Animal Nickname" \
     --notes docs/releases/X.Y.Z.md
   ```
   The script runs the tests, creates and pushes the annotated tag, publishes the
   GitHub release, moves `v1`, and pushes the moved tag.
6. Verify:
   ```bash
   bash scripts/check-release-tags.sh
   gh release view vX.Y.Z
   gh release list
   ```
7. Pin examples and the reusable workflow to the released SHA in a follow-up PR.
8. Close the release milestone and update the current position in
   `docs/versioning.md`.

## Rollback

If the release is wrong:

1. Edit the release title/body immediately.
2. If necessary, delete the GitHub release and remote tag, then recreate them.
3. Move `v1` back to the previous verified release.
4. Add a correction note to the release and affected issues.

Never move `v1` before the release page and tag target have been verified.

## Branch protection and release-manager bypass

`main` is protected: changes arrive by pull request and the `go` CI check must
pass. The repository administrator bypass is reserved for release preparation
when the release automation cannot complete through a pull request. Record the
reason in the release notes or the release issue; do not use the bypass for
normal feature or documentation changes.
