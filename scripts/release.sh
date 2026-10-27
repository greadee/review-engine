#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/release.sh --version vX.Y.Z --title "vX.Y.Z Animal Name" --notes docs/releases/X.Y.Z.md [--dry-run] [--skip-tests]

Publishes a reviewed release from main, then moves the v1 major tag.
The tag title uses the animal nickname; the release note states the purpose.
USAGE
}

fail() { printf 'release: %s\n' "$1" >&2; exit 1; }
run() {
  if $dry_run; then
    printf '+ %s\n' "$*"
  else
    "$@"
  fi
}

version=""; title=""; notes=""; dry_run=false; skip_tests=false
while [ $# -gt 0 ]; do
  case "$1" in
    --version) version="${2:-}"; shift 2 ;;
    --title) title="${2:-}"; shift 2 ;;
    --notes) notes="${2:-}"; shift 2 ;;
    --dry-run) dry_run=true; shift ;;
    --skip-tests) skip_tests=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1" ;;
  esac
done

[ -n "$version" ] || fail "--version is required"
[ -n "$title" ] || fail "--title is required"
[ -n "$notes" ] || fail "--notes is required"
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "version must look like vX.Y.Z"
[ -f "$notes" ] || fail "release note not found: $notes"
grep -q "This is the" "$notes" || fail "release note must open with the release purpose"

changelog_version="${version#v}"
grep -q "## \[${changelog_version}\]" CHANGELOG.md || fail "CHANGELOG.md has no [${changelog_version}] section"

if ! $dry_run; then
  branch="$(git symbolic-ref --short HEAD 2>/dev/null || true)"
  [ "$branch" = "main" ] || fail "release from main (current: ${branch:-detached})"
  [ -z "$(git status --porcelain)" ] || fail "working tree is not clean"
fi
git rev-parse -q --verify "refs/tags/$version" >/dev/null && fail "tag already exists: $version"

release_name="${RELEASE_GIT_NAME:-$(git config user.name 2>/dev/null || true)}"
release_email="${RELEASE_GIT_EMAIL:-$(git config user.email 2>/dev/null || true)}"
[ -n "$release_name" ] || fail "set RELEASE_GIT_NAME or git user.name"
[ -n "$release_email" ] || fail "set RELEASE_GIT_EMAIL or git user.email"
export GIT_AUTHOR_NAME="$release_name"
export GIT_AUTHOR_EMAIL="$release_email"
export GIT_COMMITTER_NAME="$release_name"
export GIT_COMMITTER_EMAIL="$release_email"

if ! $skip_tests; then
  run go test ./...
fi

run git tag -a "$version" -m "$title"
run git push origin "$version"
run gh release create "$version" --title "$title" --notes-file "$notes" --latest
run git tag -f -a v1 "$version" -m "v1 (latest v1.x)"
run git push origin refs/tags/v1 --force

if $dry_run; then
  printf 'dry run complete: %s (%s)\n' "$version" "$title"
else
  printf 'released %s; v1 moved to %s\n' "$version" "$version"
fi
