#!/usr/bin/env bash
# Generates THIRD_PARTY_LICENSES.md by resolving Go module dependencies
# via go-licenses.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT="$ROOT_DIR/THIRD_PARTY_LICENSES.md"
MODULE="github.com/oliverandrich/den"

if ! command -v go-licenses &>/dev/null; then
    echo "Error: go-licenses not found. Install with:"
    echo "  go install github.com/google/go-licenses@latest"
    exit 1
fi

cat > "$OUTPUT" <<'HEADER'
# Third-Party Licenses

This file lists all third-party Go module dependencies used by Den.

This file is auto-generated. Run `just licenses` to regenerate it.

## Go Module Dependencies

Generated with [google/go-licenses](https://github.com/google/go-licenses).

| Module | License | URL |
|--------|---------|-----|
HEADER

go-licenses report ./... 2>/dev/null \
    | sort \
    | grep -v "^${MODULE}" \
    | sed 's|^modernc.org/mathutil,Unknown,Unknown$|modernc.org/mathutil,https://gitlab.com/cznic/mathutil/-/blob/master/LICENSE,BSD-3-Clause|' \
    | while IFS=',' read -r mod url license; do
        printf '| %s | %s | [LICENSE](%s) |\n' "$mod" "$license" "$url"
    done >> "$OUTPUT"

echo "Generated $OUTPUT"
