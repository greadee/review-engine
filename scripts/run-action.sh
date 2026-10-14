#!/usr/bin/env bash
#
# Dispatches the composite GitHub Action to the `revu` binary.
#
# Security note (CWE-78): every caller-supplied value arrives through the
# environment as REVIEW_INPUT_*. Nothing is interpolated into this script's
# text, so a branch name (or any other input) containing quotes, newlines,
# command substitutions, or shell metacharacters is treated strictly as data:
# it is validated against a fixed allowlist where applicable, and otherwise
# passed to `revu` as a single, quoted argv element.
#
# GitHub's guidance: https://docs.github.com/en/actions/concepts/security/script-injections
set -euo pipefail

fail() { printf 'revu action: %s\n' "$1" >&2; exit 1; }

# require_enum NAME VALUE ALLOWED...
require_enum() {
  local name="$1" value="$2"
  shift 2
  local allowed
  for allowed in "$@"; do
    [ "$value" = "$allowed" ] && return 0
  done
  fail "invalid $name: $value (allowed: $*)"
}

# require_uint NAME VALUE (empty is allowed)
require_uint() {
  local name="$1" value="$2"
  if [ -n "$value" ]; then
    case "$value" in
      *[!0-9]*) fail "invalid $name: $value (must be a non-negative integer)" ;;
    esac
  fi
}

phase="${REVIEW_INPUT_PHASE:-all}"
require_enum phase "$phase" all collect finalize

profile="${REVIEW_INPUT_PROFILE:-pr}"
require_enum profile "$profile" pr issue sprint audit impact

provider="${REVIEW_INPUT_PROVIDER:-openai-compatible}"
model="${REVIEW_INPUT_MODEL:-}"

semantic="${REVIEW_INPUT_SEMANTIC:-true}"
publish="${REVIEW_INPUT_PUBLISH:-true}"
comment="${REVIEW_INPUT_COMMENT:-true}"
block="${REVIEW_INPUT_BLOCK:-true}"
reopen="${REVIEW_INPUT_REOPEN:-false}"
close="${REVIEW_INPUT_CLOSE:-false}"
store_sqlite="${REVIEW_INPUT_STORE_SQLITE:-false}"
for pair in "semantic:$semantic" "publish:$publish" "comment:$comment" \
            "block:$block" "reopen:$reopen" "close:$close" \
            "store-sqlite:$store_sqlite"; do
  require_enum "${pair%%:*}" "${pair#*:}" true false
done

threshold="${REVIEW_INPUT_BLOCK_THRESHOLD:-P1}"
require_enum block-threshold "$threshold" P0 P1 P2 P3

pr="${REVIEW_INPUT_PR:-}"
issue="${REVIEW_INPUT_ISSUE:-}"
require_uint pr "$pr"
require_uint issue "$issue"

config="${REVIEW_INPUT_CONFIG:-revu.yaml}"
base="${REVIEW_INPUT_BASE:-origin/main}"
head="${REVIEW_INPUT_HEAD:-HEAD}"
repository="${REVIEW_INPUT_REPOSITORY:-}"
bundle="${REVIEW_INPUT_BUNDLE:-bundle.json}"
archive_branch="${REVIEW_INPUT_ARCHIVE_BRANCH:-review-artifacts}"

revu="${REVU_BIN:-${RUNNER_TEMP:-/tmp}/revu}"

# Fold the validated inputs into the REVIEW_* variables the engine reads, so the
# configuration has a single source of truth.
export REVIEW_PROFILE="$profile"
export REVIEW_PROVIDER="$provider"
export REVIEW_MODEL="$model"
export REVIEW_SEMANTIC="$semantic"
export REVIEW_ARCHIVE_BRANCH="$archive_branch"
export REVIEW_BLOCK_THRESHOLD="$threshold"
export REVIEW_STORE_SQLITE="$store_sqlite"
export REVIEW_ISSUE_AUTO_CLOSE="$close"
export REVIEW_ISSUE_AUTO_REOPEN="$reopen"

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "summary=summary.md" >> "$GITHUB_OUTPUT"
fi

case "$phase" in
  collect)
    "$revu" collect \
      --config "$config" \
      --profile "$profile" \
      --base "$base" \
      --head "$head" \
      --repository "$repository" \
      --out "$bundle"
    exit $?
    ;;
  finalize)
    args=(finalize
      --config "$config"
      --bundle "$bundle"
      --repository "$repository"
      --out summary.md
      --json findings.json)
    [ -n "$pr" ] && args+=(--pr "$pr")
    [ "$publish" = "true" ] && args+=(--publish)
    [ "$comment" = "true" ] && args+=(--comment)
    [ "$block" = "true" ] || args+=(--no-block)
    "$revu" "${args[@]}"
    exit $?
    ;;
esac

if [ "$profile" = "issue" ]; then
  args=(issue
    --config "$config"
    --repository "$repository"
    --issue "$issue"
    --out summary.md)
  [ "$comment" = "true" ] && args+=(--comment)
  [ "$reopen" = "true" ] && args+=(--reopen)
  [ "$close" = "true" ] && args+=(--close)
  [ "$block" = "true" ] && args+=(--block)
  "$revu" "${args[@]}"
  exit $?
fi

args=(run
  --config "$config"
  --profile "$profile"
  --base "$base"
  --head "$head"
  --repository "$repository"
  --out summary.md
  --json findings.json)
[ -n "$pr" ] && args+=(--pr "$pr")
[ "$publish" = "true" ] && args+=(--publish)
[ "$comment" = "true" ] && args+=(--comment)
[ "$block" = "true" ] || args+=(--no-block)
"$revu" "${args[@]}"
