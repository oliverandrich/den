# Contributing

Contributions to Den are welcome. This guide covers the development setup and workflow.

---

## Prerequisites

- **Go 1.25+** -- [download](https://go.dev/dl/)
- **just** -- command runner ([install](https://github.com/casey/just))
- **golangci-lint** -- Go linter ([install](https://golangci-lint.run/welcome/install/))
- **PostgreSQL** -- required for running the full test suite

## Development Setup

Clone the repository and verify that all required tools are installed:

```bash
git clone https://github.com/oliverandrich/den.git
cd den
just setup
```

`just setup` checks for all required development tools and reports any that are missing.

---

## Running Tests

```bash
# Run all tests (SQLite + PostgreSQL)
just test

# Run tests with coverage report
just coverage
```

Tests run against both the SQLite and PostgreSQL backends. A local PostgreSQL instance must be available for the full test suite.

---

## Linting

```bash
just lint
```

Runs `golangci-lint run ./...`. All code must pass linting before merge.

---

## Formatting

```bash
just fmt
```

Runs `gofmt` and `goimports` on all Go files.

---

## Commit Messages

This project uses [Conventional Commits](https://www.conventionalcommits.org/). Every commit message must follow this format:

```
<type>: <description>

[optional body]
```

**Types:**

| Type | Description |
|---|---|
| `feat` | New feature |
| `fix` | Bug fix |
| `docs` | Documentation changes |
| `refactor` | Code restructuring without behavior change |
| `test` | Adding or updating tests |
| `chore` | Build, CI, or tooling changes |
| `perf` | Performance improvement |

**Examples:**

```
feat: add StringContains operator for string fields
fix: handle nil pointer in link resolution
docs: update API reference with aggregation methods
```

---

## Pull Requests

1. Fork the repository
2. Create a feature branch from `main`
3. Make your changes with tests
4. Ensure `just test` and `just lint` pass
5. Submit a pull request

---

## Reporting Issues

Please file issues on [GitHub Issues](https://github.com/oliverandrich/den/issues). Include:

- Go version (`go version`)
- Backend (SQLite or PostgreSQL)
- Minimal reproduction case
- Expected vs actual behavior
