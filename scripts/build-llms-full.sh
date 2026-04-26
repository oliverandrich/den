#!/usr/bin/env bash
# Regenerates docs/llms-full.txt by concatenating the individual doc
# pages in mkdocs navigation order, separated by "---" dividers.
#
# Run this after editing any docs/*.md so the single-file mirror stays
# in sync. Keep the PAGES list aligned with the nav: in mkdocs.yml.
set -euo pipefail

cd "$(dirname "$0")/.."

OUT=docs/llms-full.txt

PAGES=(
  docs/index.md
  docs/getting-started/installation.md
  docs/getting-started/mental-model.md
  docs/getting-started/quickstart.md
  docs/getting-started/tour.md
  docs/getting-started/backends.md
  docs/guide/documents.md
  docs/guide/crud.md
  docs/guide/queries.md
  docs/guide/relations.md
  docs/guide/aggregations.md
  docs/guide/full-text-search.md
  docs/guide/attachments.md
  docs/guide/storage/file.md
  docs/guide/storage/s3.md
  docs/guide/storage/custom.md
  docs/guide/transactions.md
  docs/guide/hooks.md
  docs/guide/soft-delete.md
  docs/guide/change-tracking.md
  docs/guide/revision-control.md
  docs/guide/validation.md
  docs/guide/migrations.md
  docs/guide/testing.md
  docs/guide/recipes.md
  docs/reference/api.md
  docs/reference/operators.md
  docs/reference/errors.md
  docs/reference/struct-tags.md
  docs/reference/configuration.md
)

: > "$OUT"
for i in "${!PAGES[@]}"; do
  page=${PAGES[$i]}
  if [[ ! -f $page ]]; then
    echo "missing page: $page" >&2
    exit 1
  fi
  if [[ $i -gt 0 ]]; then
    printf -- '\n---\n\n' >> "$OUT"
  fi
  cat "$page" >> "$OUT"
done

echo "wrote $OUT ($(wc -l < "$OUT") lines from ${#PAGES[@]} pages)"
