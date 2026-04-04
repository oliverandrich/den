# Default recipe: list available commands
default:
    @just --list

# Run all tests (SQLite + PostgreSQL)
test *args:
    go test -race -json {{args}} ./... | tparse

# Run linter
lint:
    golangci-lint run ./...

# Format all Go files
fmt:
    gofmt -w .
    goimports -w .

# Run tests with coverage (SQLite + PostgreSQL)
coverage:
    #!/usr/bin/env bash
    set -euo pipefail
    go test -race -json -coverprofile=coverage.out ./... > test.json
    tparse -file=test.json
    rm -f test.json
    go tool cover -html=coverage.out -o coverage.html
    echo "Coverage report: coverage.html"

# Tidy module dependencies
tidy:
    go mod tidy

# Run vulnerability check
vuln:
    govulncheck ./...

# List active beans (excludes completed and scrapped)
beans:
    beans list --no-status completed --no-status scrapped

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
