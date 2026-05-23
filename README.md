# Den

<p align="center">
  <img src="docs/assets/cover.jpg" alt="Go gophers organizing documents in their den" width="600">
  <br>
  <em>"Every <a href="https://github.com/oliverandrich/burrow">burrow</a> needs a den — a place to store what matters and find it again when you need it."</em>
</p>

<p align="center">
  <a href="https://github.com/oliverandrich/den/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/oliverandrich/den/ci.yml?branch=main&label=CI&style=for-the-badge" alt="CI"></a>
  <a href="https://github.com/oliverandrich/den/releases"><img src="https://img.shields.io/github/v/release/oliverandrich/den?style=for-the-badge" alt="Release"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/github/go-mod/go-version/oliverandrich/den?style=for-the-badge" alt="Go Version"></a>
  <a href="https://goreportcard.com/report/github.com/oliverandrich/den"><img src="https://goreportcard.com/badge/github.com/oliverandrich/den?style=for-the-badge" alt="Go Report Card"></a>
  <a href="/LICENSE"><img src="https://img.shields.io/github/license/oliverandrich/den?style=for-the-badge" alt="License"></a>
  <a href="https://den-odm.readthedocs.io/"><img src="https://img.shields.io/badge/docs-den--odm.readthedocs.io-blue?style=for-the-badge" alt="Docs"></a>
</p>

An ODM for Go with two storage backends — SQLite and PostgreSQL. Same API, your choice of engine.

Each Go struct you register is a *document*, stored as a JSONB row in a SQL table that Den calls a *collection*. The SQL schema is one table per type with a JSONB `data` column plus a small set of secondary indexes Den maintains for you. You query collections with a fluent builder, relate them with typed links, and run it all in transactions. The SQLite backend compiles into your binary with no external dependencies. The PostgreSQL backend connects to your existing database. Switch between them by changing one line.

> [!NOTE]
> Den is a document store, not a relational database. It does not support SQL, JOINs, or schema migrations in the traditional sense. If you need relational modeling, use [Bun](https://bun.uptrace.dev/) or [GORM](https://gorm.io/) instead.

## Features

- **Two backends, one API** — SQLite (embedded, pure Go, no CGO) and PostgreSQL (server-based, JSONB + GIN indexes)
- **Chainable QuerySet** — `NewQuery[T](db).Where(...).Sort(...).Limit(n).All(ctx)` with lazy evaluation
- **Range iteration** — `Iter()` returns `iter.Seq2[*T, error]` for memory-efficient streaming with Go's `range`
- **Typed relations** — `Link[T]` for one-to-one, `[]Link[T]` for one-to-many, with cascade write/delete and eager/lazy fetch
- **Back-references** — `QuerySet.BackLinks` finds all documents referencing a given target
- **Native aggregation** — `Avg`, `Sum`, `Min`, `Max` pushed down to SQL; `GroupBy` and `Project` for analytics
- **Full-text search** — FTS5 for SQLite, tsvector for PostgreSQL, same `Search()` API
- **Lifecycle hooks** — BeforeInsert, AfterUpdate, Validate, and more — interfaces on your struct, no registration
- **Change tracking** — opt-in via `Tracked`: `IsChanged`, `GetChanges`, `Revert` with byte-level snapshots
- **Soft delete** — embed `SoftDelete` alongside `Base`, automatic query filtering, `HardDelete` for permanent removal
- **Attachments & storage** — embed `Attachment`, install a `den.Storage` backend once, let the hard-delete cascade clean bytes automatically
- **Optimistic concurrency** — revision-based conflict detection with `ErrRevisionConflict`
- **Transactions** — `RunInTransaction` with panic-safe rollback
- **Migrations** — registry-based, each migration runs atomically in a transaction
- **Struct tag validation** — `validate:"required,email"` tags via `go-playground/validator`, always-on; no opt-in, no bypass from inside Den
- **Expression indexes** — `den:"index"`, `den:"unique"`, nullable unique for pointer fields

## Quick Start

```bash
mkdir myapp && cd myapp
go mod init myapp
go get github.com/oliverandrich/den@latest
```

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/oliverandrich/den"
    _ "github.com/oliverandrich/den/backend/sqlite" // register sqlite:// scheme
    "github.com/oliverandrich/den/document"
    "github.com/oliverandrich/den/where"
)

type Product struct {
    document.Base
    Name  string  `json:"name"  den:"index"`
    Price float64 `json:"price" den:"index"`
}

func main() {
    ctx := context.Background()

    // Open a SQLite database
    db, err := den.OpenURL(ctx, "sqlite:///products.db")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Register document types (creates tables and indexes)
    if err := den.Register(ctx, db, &Product{}); err != nil {
        log.Fatal(err)
    }

    // Save — empty ID → insert, non-empty ID → update. Same call covers both.
    p := &Product{Name: "Widget", Price: 9.99}
    if err := den.Save(ctx, db, p); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Saved: %s (ID: %s)\n", p.Name, p.ID)

    // Query
    products, err := den.NewQuery[Product](db,
        where.Field("price").Lt(20.0),
    ).Sort("name", den.Asc).All(ctx)
    if err != nil {
        log.Fatal(err)
    }
    for _, prod := range products {
        fmt.Printf("  %s — $%.2f\n", prod.Name, prod.Price)
    }

    // Iterate (streaming, memory-efficient)
    for doc, err := range den.NewQuery[Product](db).Iter(ctx) {
        if err != nil {
            log.Fatal(err)
        }
        fmt.Printf("  %s\n", doc.Name)
    }
}
```

To use PostgreSQL instead, change the DSN and the import:

```go
import _ "github.com/oliverandrich/den/backend/postgres" // instead of sqlite

db, err := den.OpenURL(ctx, "postgres://user:pass@localhost/mydb")
```

## Architecture

```
den/
├── den.go, crud.go, query.go       Public API: Open, Save, FindByID, NewQuery, …
├── aliases.go, options.go          Type aliases + CRUDOption / LockOption constructors
├── errors.go                       Error sentinels (re-exports)
├── engine/                         Engine — every implementation file, plus tests
├── internal/util/                  Shared helpers (reflect, sql safety, field validation)
├── storage/                        Storage backend registry + OpenURL
├── storage/file/                   Local filesystem backend (file:// scheme)
├── document/                       Base + composable SoftDelete, Tracked, Attachment embeds
├── where/                          Query condition builders
├── backend/
│   ├── sqlite/                     SQLite backend (pure Go, no CGO)
│   └── postgres/                   PostgreSQL backend (pgx)
├── validate/                       Struct tag validation entry point
├── migrate/                        Migration framework
└── dentest/                        Test helpers
```

The root package is a thin convenience surface — six files of type aliases and one-line wrapper functions. Everything load-bearing lives in `engine/`, a public package that applications can import directly when they need access to types the root doesn't surface. Aliases preserve identity (`den.QuerySet[T]` IS `engine.QuerySet[T]`), so the indirection is free at runtime.

### Backend Interface

Both backends implement the same `Backend` interface. The `ReadWriter` subset is shared between backends and transactions, so CRUD code works identically inside and outside transactions.

```go
type ReadWriter interface {
    Get(ctx, collection, id) ([]byte, error)
    Put(ctx, collection, id, data) error
    Delete(ctx, collection, id) error
    Query(ctx, collection, *Query) (Iterator, error)
    Count(ctx, collection, *Query) (int64, error)
    Exists(ctx, collection, *Query) (bool, error)
    Aggregate(ctx, collection, op, field, *Query) (*float64, error)
}
```

### Document Types

Every document embeds `document.Base` — the required anchor that provides
`ID`, `CreatedAt`, `UpdatedAt`, `Rev`. Opt-in features are available as
separate composable embeds:

| Embed | Purpose |
|---|---|
| `document.Base` | Required. Provides `ID`, `CreatedAt`, `UpdatedAt`, `Rev` |
| `document.SoftDelete` | Adds `DeletedAt` and `IsDeleted()` for non-destructive deletion |
| `document.Tracked` | Adds byte-snapshot machinery for `IsChanged`, `GetChanges`, `Revert` |
| `document.Attachment` | Adds `StoragePath`, `Mime`, `Size`, `SHA256` — file reference paired with a `den.Storage` backend |

Compose freely: `struct { document.Base; document.SoftDelete; document.Tracked; document.Attachment; ... }`.

### Query Operators

```go
where.Field("price").Gt(10)           // comparison
where.Field("status").In("a", "b")    // set membership
where.Field("tags").Contains("go")    // array contains
where.Field("email").IsNil()          // null check
where.Field("name").RegExp("^W")      // regular expression
where.And(cond1, cond2)               // logical combinators
where.Field("addr.city").Eq("Berlin") // nested fields (dot notation)
```

## Validation

Den runs `validate` struct-tag constraints automatically on every Save — there is no opt-in and no way to bypass them from inside Den. Add tags via [`go-playground/validator`](https://github.com/go-playground/validator):

```go
type User struct {
    document.Base
    Name  string `json:"name"  den:"unique" validate:"required,min=3,max=50"`
    Email string `json:"email" den:"unique" validate:"required,email"`
    Age   int    `json:"age"                validate:"gte=0,lte=130"`
}
```

Errors wrap `den.ErrValidation` and can be inspected for field-level detail:

```go
err := den.Save(ctx, db, &User{Name: "ab"})
if errors.Is(err, den.ErrValidation) {
    var ve *validate.Errors
    if errors.As(err, &ve) {
        for _, fe := range ve.Fields {
            fmt.Printf("%s failed on %s\n", fe.Field, fe.Tag)
        }
    }
}
```

Tag validation and the `Validator` interface coexist — tag validation runs first (structural rules), then `Validate()` (business logic). The `validate/` package also exports `validate.Document(doc)` for callers that want to run the same checks outside the Den boundary (HTTP handlers, form parsers). For validating arbitrary non-document structs, use [`go-playground/validator/v10`](https://github.com/go-playground/validator) directly.

## Testing

Den provides a `dentest` package for test setup:

```go
func TestMyFeature(t *testing.T) {
    db := dentest.MustOpen(t, &Product{}, &Category{})
    // File-backed SQLite in t.TempDir(), auto-closed via t.Cleanup
}
```

For PostgreSQL tests:

```go
func TestMyFeature(t *testing.T) {
    db := dentest.MustOpenPostgres(t, "postgres://localhost/test", &Product{})
}
```

## Benchmarks

Measured on an Apple M4 Pro (14 cores), Go 1.25, PostgreSQL 17 on localhost. The fixture is a ~1 KB article document (title, body, status, category, tags, price, indexed timestamp, embedded author link, metadata map) — closer to a real blog or catalog entry than a minimal struct.

Reproduce locally with `mise run bench-readme`. Numbers exclude connection-setup overhead (the bench helper opens the DB once and reuses it).

### Serial workloads

Single-goroutine latency per operation. Lower is better.

<!-- BENCH:SERIAL -->
| Scenario | SQLite | Postgres | SQLite allocs | Postgres allocs |
|---|---:|---:|---:|---:|
| Save (insert) | 150.1 µs | 159.0 µs | 49 | 47 |
| SaveAll (100) | 9.95 ms | 13.58 ms | 5211 | 4716 |
| SaveAll (1000) | 91.94 ms | 139.92 ms | 52026 | 47071 |
| FindByID | 104.0 µs | 424.4 µs | 62 | 59 |
| FindByIDs (10) | 264.2 µs | 947.7 µs | 343 | 329 |
| Query + Sort + Limit(10) | 723.8 µs | 1.84 ms | 327 | 291 |
| Query + Sort + Limit(100) | 1.90 ms | 4.69 ms | 2939 | 2544 |
| Iter (1000 rows) | 2.65 ms | 2.78 ms | 29044 | 25029 |
| Count(filter) | 25.3 µs | 780.7 µs | 29 | 31 |
| Sum(filter) | 177.2 µs | 1.04 ms | 35 | 41 |
| FTS Search | 902.6 µs | 2.13 ms | 604 | 513 |
| WithFetchLinks (20 rows) | 74.0 µs | 438.5 µs | 656 | 570 |
| Save (update) | 140.3 µs | 349.8 µs | 100 | 96 |
| QuerySet.Update (100) | 9.28 ms | 21.22 ms | 7049 | 6143 |
| RunInTransaction | 181.0 µs | 330.1 µs | 116 | 102 |

<!-- /BENCH:SERIAL -->

### Concurrent workloads

`b.RunParallel` with Go's default `GOMAXPROCS`. Higher ops/sec is better. SQLite serializes writers by design (BEGIN IMMEDIATE), so write-heavy numbers plateau at single-writer speed; PostgreSQL's MVCC scales writes across connections.

<!-- BENCH:CONCURRENT -->
| Scenario | SQLite | Postgres |
|---|---:|---:|
| FindByID | 8.6k ops/s | 3.1k ops/s |
| Save (insert) | 4.7k ops/s | 30.3k ops/s |
| Mixed reads/writes 80/20 | 14.8k ops/s | 3.7k ops/s |
| Queue consumer (SkipLocked) | 24.7k ops/s | 21.9k ops/s |

<!-- /BENCH:CONCURRENT -->

## Development

Den uses [mise](https://mise.jdx.dev/) for tool pinning and task running. `.mise.toml` pins the Go toolchain plus `tparse`, `golangci-lint`, `goimports`, `govulncheck`, `go-licenses`, and `pre-commit`:

```bash
mise install                # Install pinned tools from .mise.toml
mise run setup              # Verify dev environment (also installs tools)
mise run test               # Run all tests (SQLite + PostgreSQL)
mise run lint               # Run golangci-lint
mise run fmt                # Format all Go files
mise run coverage           # Run tests with coverage report
mise run coverage-check     # Enforce per-package coverage threshold
mise run vuln               # Run vulnerability check
mise run tidy               # Tidy module dependencies
mise run beans              # List active beans (issue tracker)
```

Requires Go 1.25+ (managed by mise). Run `mise run setup` to verify your dev environment.

## Dependencies

| Dependency | Purpose |
|---|---|
| `modernc.org/sqlite` | SQLite backend (pure Go, no CGO) |
| `github.com/jackc/pgx/v5` | PostgreSQL backend |
| `github.com/go-playground/validator/v10` | Struct tag validation (optional, via `den/validate`) |

## License

Den is licensed under the [MIT License](LICENSE).

The Go Gopher was originally designed by [Renee French](https://reneefrench.blogspot.com/) and is licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).
