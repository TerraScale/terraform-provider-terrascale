#!/usr/bin/env bash
set -euo pipefail

binary=$1
metadata=$(go version -m "$binary")
printf '%s\n' "$metadata"
expected="go$(tr -d '\n' < .go-version)"
actual=$(head -n 1 <<< "$metadata")
if [[ "$actual" != *": $expected" ]]; then
  echo "Release binary must use $expected" >&2
  exit 1
fi
govulncheck -mode=binary -scan=module -show=verbose "$binary"
