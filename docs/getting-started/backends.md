# Backends

Den supports two storage backends behind a unified API. Both store documents as JSONB and provide full-text search, indexing, and transactions.

## Comparison

| | SQLite | PostgreSQL |
|---|---|---|
| **Type** | Embedded | Server-based |
| **CGO required** | No (pure Go via `modernc.org/sqlite`) | No (`pgx` is pure Go) |
| **External dependencies** | None | Running PostgreSQL instance |
| **JSON storage** | JSONB | JSONB + GIN indexes |
| **Full-text search** | FTS5 | tsvector |
| **Concurrency** | Single-writer, multiple readers | Full MVCC |
| **Best for** | CLI tools, single-binary deployments, dev/test | Multi-user apps, replication, scale |

## When to Use Which

**Choose SQLite when:**

- You want a single binary with no external services
- Your application has a single writer (CLI tools, desktop apps, APIs with low write concurrency)
- You need a zero-config development or testing setup

**Choose PostgreSQL when:**

- Multiple processes or services write concurrently
- You need replication, backups, or high availability
- You are already running PostgreSQL in your infrastructure

!!! tip "Start with SQLite, switch later"
    Since both backends share the same API, you can prototype with SQLite and move to PostgreSQL when your requirements grow. The switch is a one-line change.

## DSN Formats

=== "SQLite"

    ```go
    // File-based database
    db, err := den.OpenURL("sqlite:///path/to/data.db")

    // Relative path
    db, err := den.OpenURL("sqlite:///./local.db")
    ```

    The path after `sqlite:///` is passed directly to the SQLite driver. Use an absolute path for production and a relative path for development.

=== "PostgreSQL"

    ```go
    // Standard connection string
    db, err := den.OpenURL("postgres://user:pass@localhost:5432/mydb")

    // With SSL mode
    db, err := den.OpenURL("postgres://user:pass@localhost/mydb?sslmode=disable")

    // Unix socket
    db, err := den.OpenURL("postgres:///mydb?host=/var/run/postgresql")
    ```

    The DSN follows the [libpq connection string](https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING) format supported by pgx.

## Opening a Database

Import the backend package for its side-effect registration, then call `den.OpenURL`:

=== "SQLite"

    ```go
    import (
        "github.com/oliverandrich/den"
        _ "github.com/oliverandrich/den/backend/sqlite"
    )

    db, err := den.OpenURL("sqlite:///data.db")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()
    ```

=== "PostgreSQL"

    ```go
    import (
        "github.com/oliverandrich/den"
        _ "github.com/oliverandrich/den/backend/postgres"
    )

    db, err := den.OpenURL("postgres://user:pass@localhost/mydb")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()
    ```

!!! warning "Unregistered backend"
    If you call `den.OpenURL` with a scheme that has no registered backend (e.g., you forgot the blank import), you will get an error at runtime. Make sure the corresponding `_ "github.com/oliverandrich/den/backend/..."` import is present.

## Backend-Specific Behavior

While the API is identical, some internal behaviors differ:

| Feature | SQLite | PostgreSQL |
|---|---|---|
| **Array index queries** | JSON `each()` | GIN `@>` operator |
| **FTS indexing** | FTS5 virtual table with `MATCH` | tsvector column with `@@` |
| **Regex** | `REGEXP` function | `~` operator |
| **Unique nulls** | Handled via partial index | Handled via partial index |

These differences are transparent to your application code. The `where` package and `Search()` API abstract them away.
