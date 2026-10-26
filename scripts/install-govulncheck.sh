#!/usr/bin/env bash
set -euo pipefail

# Cache only the pinned executable; every scan queries the live advisory feed.
scanner_dir="$HOME/.cache/terrascale/govulncheck"
mkdir -p "$scanner_dir"
if [[ ! -x "$scanner_dir/govulncheck" ]]; then
  GOBIN="$scanner_dir" go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
fi
echo "$scanner_dir" >> "$GITHUB_PATH"
