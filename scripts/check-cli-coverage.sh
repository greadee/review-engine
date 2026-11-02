#!/usr/bin/env bash
# Enforce the documented cmd/revu coverage floor.
set -euo pipefail

floor="${CLI_COVERAGE_FLOOR:-60.0}"
profile="$(mktemp)"
trap 'rm -f "$profile"' EXIT

go test ./cmd/revu -coverprofile="$profile" >/dev/null
total="$(go tool cover -func="$profile" | awk '/^total:/ {gsub(/%/, "", $3); print $3}')"

if [[ -z "$total" ]]; then
  echo "coverage check: could not read total coverage" >&2
  exit 1
fi

if awk -v total="$total" -v floor="$floor" 'BEGIN { exit !(total + 0 >= floor + 0) }'; then
  printf 'cmd/revu coverage: %s%% (floor %s%%)\n' "$total" "$floor"
else
  printf 'cmd/revu coverage: %s%% is below the documented floor of %s%%\n' "$total" "$floor" >&2
  exit 1
fi
