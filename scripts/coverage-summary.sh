#!/usr/bin/env bash
# Print per-source-package coverage from a merged coverage profile.
# Format: "<package> <percentage>" (one line per package, no % sign,
# one decimal place). Sorted by package path.
#
# Used by `just coverage` (formats the output as a table) and
# `just coverage-check` (compares against a numeric threshold).
#
# Why this script exists: when `go test -coverpkg=./...` is run per
# module and the resulting coverage.out files are concatenated, every
# statement range appears once per test package that instrumented it
# — most copies with count=0. Naive aggregation inflates totals and
# tanks the percentages. The awk pass below dedups by range key,
# taking max count, before computing per-package coverage.
set -euo pipefail
profile="${1:-coverage.out}"
awk 'NR > 1 {
    key = $1
    if (!(key in stmts)) { stmts[key] = $2 }
    if ($3 > maxc[key]) { maxc[key] = $3 }
} END {
    for (key in stmts) {
        split(key, a, ":")
        n = split(a[1], parts, "/")
        pkg = parts[1]
        for (i = 2; i < n; i++) pkg = pkg "/" parts[i]
        total[pkg] += stmts[key]
        if (maxc[key] > 0) covered[pkg] += stmts[key]
    }
    for (p in total) printf "%s %.1f\n", p, (covered[p] / total[p]) * 100
}' "$profile" | sort
