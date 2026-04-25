# Default recipe: list available commands
default:
    @just --list

# Modules tracked by go.work. Extend when adding new storage/* submodules.
mods := ". ./storage/s3"

# Run all tests across all modules (SQLite + PostgreSQL)
test *args:
    #!/usr/bin/env bash
    set -euo pipefail
    for mod in {{mods}}; do
        echo "==> tests: $mod"
        (cd "$mod" && go test -race -json {{args}} ./...) | tparse
    done

# Run linter across all modules
lint:
    #!/usr/bin/env bash
    set -euo pipefail
    for mod in {{mods}}; do
        echo "==> lint: $mod"
        (cd "$mod" && golangci-lint run ./...)
    done

# Format all Go files
fmt:
    gofmt -w .
    goimports -w .

# Run tests with coverage across all modules and merge into one report
coverage:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "mode: atomic" > coverage.out
    for mod in {{mods}}; do
        echo "==> coverage: $mod"
        (
            cd "$mod"
            go test -race -json -coverprofile=cover.out ./... > test.json
            tparse -file=test.json
            rm -f test.json
        )
        # Append per-module coverage lines, dropping the duplicate mode header.
        if [ -f "$mod/cover.out" ]; then
            tail -n +2 "$mod/cover.out" >> coverage.out
            rm -f "$mod/cover.out"
        fi
    done
    go tool cover -html=coverage.out -o coverage.html
    echo "Coverage report: coverage.html"

# Run benchmarks (use count=N for statistical significance)
bench count="1":
    go test -bench=. -benchmem -count={{count}} -run='^$$' .

# Run realworld + concurrent benchmarks and refresh the README tables.
# Requires a reachable PostgreSQL via DEN_POSTGRES_URL; falls back to the
# default local DSN used by dentest when unset.
bench-readme:
    #!/usr/bin/env bash
    set -euo pipefail
    out=$(mktemp -t den-bench.XXXXXX)
    trap 'rm -f "$out"' EXIT
    go test -bench='Benchmark(RW|Concurrent)_' -benchmem -run='^$$' -benchtime=2s . | tee "$out"
    go run ./scripts/bench_report.go -readme=README.md < "$out"
    echo "README.md benchmark tables updated."

# Tidy module dependencies across all modules and sync the workspace
tidy:
    #!/usr/bin/env bash
    set -euo pipefail
    for mod in {{mods}}; do
        (cd "$mod" && go mod tidy)
    done
    go work sync

# Run vulnerability check across all modules
vuln:
    #!/usr/bin/env bash
    set -euo pipefail
    for mod in {{mods}}; do
        echo "==> vuln: $mod"
        (cd "$mod" && govulncheck ./...)
    done

# List active beans (excludes completed and scrapped)
beans:
    beans list --no-status completed --no-status scrapped

# Serve documentation locally
docs:
    cp CHANGELOG.md docs/changelog.md
    cp THIRD_PARTY_LICENSES.md docs/third-party-licenses.md
    uv run --with zensical zensical serve -a localhost:3000

# Build documentation
docs-build:
    cp CHANGELOG.md docs/changelog.md
    cp THIRD_PARTY_LICENSES.md docs/third-party-licenses.md
    uv run --with zensical zensical build

# Regenerate docs/llms-full.txt from the individual doc pages
llms:
    ./scripts/build-llms-full.sh

# Regenerate THIRD_PARTY_LICENSES.md from go-licenses
licenses:
    ./scripts/generate-licenses.sh

# Check that all required dev tools are installed
setup:
    #!/usr/bin/env bash
    set -euo pipefail
    ok=true
    check() {
        if command -v "$1" &>/dev/null; then
            printf "  %-20s %s\n" "$1" "$(command -v "$1")"
        else
            printf "  %-20s MISSING — %s\n" "$1" "$2"
            ok=false
        fi
    }
    echo "Checking dev tools:"
    check go              "https://go.dev/dl/"
    check golangci-lint   "go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
    check tparse          "go install github.com/mfridman/tparse@latest"
    check goimports       "go install golang.org/x/tools/cmd/goimports@latest"
    check govulncheck     "go install golang.org/x/vuln/cmd/govulncheck@latest"
    check pre-commit      "https://pre-commit.com/#install"
    echo ""
    if $ok; then
        echo "All tools installed."
        echo "Run 'pre-commit install' to set up git hooks."
    else
        echo "Some tools are missing. Install them and re-run 'just setup'."
        exit 1
    fi
