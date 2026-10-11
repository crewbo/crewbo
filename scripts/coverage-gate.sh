#!/usr/bin/env bash
# Fails when a gated package is below the coverage threshold (CLAUDE.md:
# claims, shellparse, hooks and dispatch need at least 85%). Gated packages
# that do not exist yet are skipped. Usage: scripts/coverage-gate.sh [min]
set -euo pipefail
min=${1:-85}
gated=(./internal/claims ./internal/shellparse ./internal/hooks/... ./internal/dispatch)
fail=0
for pkg in "${gated[@]}"; do
  dir=${pkg%/...}
  if ! compgen -G "$dir/*.go" >/dev/null && ! compgen -G "$dir/*/*.go" >/dev/null; then
    echo "skip  $pkg (no Go files yet)"
    continue
  fi
  while read -r name pct; do
    if [ "$pct" = "none" ]; then
      echo "FAIL  $name: no test coverage"
      fail=1
    elif awk -v p="$pct" -v m="$min" 'BEGIN { exit !(p + 0 < m + 0) }'; then
      echo "FAIL  $name: ${pct}% < ${min}%"
      fail=1
    else
      echo "ok    $name: ${pct}%"
    fi
  done < <(go test -count=1 -cover "$pkg" | awk '
    /coverage: [0-9.]+% of statements/ { for (i = 1; i <= NF; i++) if ($i ~ /^github\.com\//) name = $i; match($0, /[0-9.]+%/); print name, substr($0, RSTART, RLENGTH - 1); next }
    /no test files/ { for (i = 1; i <= NF; i++) if ($i ~ /^github\.com\//) print $i, "none" }')
done
exit "$fail"
