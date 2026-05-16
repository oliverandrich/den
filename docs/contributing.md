# Contributing

Contributions to Den are welcome. This guide covers the development setup and workflow.

---

## Prerequisites

- **mise** -- tool version manager and task runner ([install](https://mise.jdx.dev/getting-started.html))
- **PostgreSQL** -- required for running the full test suite

mise installs the pinned Go toolchain plus `golangci-lint`, `tparse`, `goimports`, `govulncheck`, `go-licenses`, and `pre-commit` from `.mise.toml`. No separate installs needed.

## Development Setup

Clone the repository and let mise install the pinned tools:

```bash
git clone https://github.com/oliverandrich/den.git
cd den
mise install
mise run setup
```

`mise run setup` installs the pinned tools and reports anything missing. Run `pre-commit install` afterwards to wire up the git hooks.

---

## Running Tests

```bash
# Run all tests (SQLite + PostgreSQL)
mise run test

# Run tests with coverage report
mise run coverage
```

Tests run against both the SQLite and PostgreSQL backends. A local PostgreSQL instance must be available for the full test suite.

---

## Linting

```bash
mise run lint
```

Runs `golangci-lint run ./...`. All code must pass linting before merge.

---

## Formatting

```bash
mise run fmt
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
4. Ensure `mise run test` and `mise run lint` pass
5. Submit a pull request

---

## Reporting Issues

Please file issues on [GitHub Issues](https://github.com/oliverandrich/den/issues). Include:

- Go version (`go version`)
- Backend (SQLite or PostgreSQL)
- Minimal reproduction case
- Expected vs actual behavior
