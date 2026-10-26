#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'release-check: %s\n' "$1" >&2; exit 1; }

latest="$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n 1)"
[ -n "$latest" ] || fail "no release tag found"

v1_sha="$(git rev-list -n 1 v1 2>/dev/null || true)"
[ -n "$v1_sha" ] || fail "v1 tag is missing"

latest_sha="$(git rev-list -n 1 "$latest")"
[ "$v1_sha" = "$latest_sha" ] || fail "v1 ($v1_sha) does not point at the latest release $latest ($latest_sha)"

notes="docs/releases/${latest#v}.md"
[ -f "$notes" ] || fail "release note missing for $latest: $notes"

printf 'release-check: v1 -> %s (%s); notes: %s\n' "$latest" "$latest_sha" "$notes"
