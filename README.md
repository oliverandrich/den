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

Den provides a MongoDB/Beanie-style document model using native Go structs. Store documents as JSONB, query them with a fluent builder, relate them with typed links, and run it all in transactions. The SQLite backend compiles into your binary with no external dependencies. The PostgreSQL backend connects to your existing database. Switch between them by changing one line.

> [!NOTE]
> Den is a document store, not a relational database. It does not support SQL, JOINs, or schema migrations in the traditional sense. If you need relational modeling, use [Bun](https://bun.uptrace.dev/) or [GORM](https://gorm.io/) instead.

## Features

- **Two backends, one API** — SQLite (embedded, pure Go, no CGO) and PostgreSQL (server-based, JSONB + GIN indexes)
- **Chainable QuerySet** — `NewQuery[T](db).Where(...).Sort(...).Limit(n).All(ctx)` with lazy evaluation
- **Range iteration** — `Iter()` returns `iter.Seq2[*T, error]` for memory-efficient streaming with Go's `range`
- **Typed relations** — `Link[T]` for one-to-one, `[]Link[T]` for one-to-many, with cascade write/delete and eager/lazy fetch
- **Back-references** — `BackLinks[T]` finds all documents referencing a given target
- **Native aggregation** — `Avg`, `Sum`, `Min`, `Max` pushed down to SQL; `GroupBy` and `Project` for analytics
- **Full-text search** — FTS5 for SQLite, tsvector for PostgreSQL, same `Search()` API
- **Lifecycle hooks** — BeforeInsert, AfterUpdate, Validate, and more — interfaces on your struct, no registration
- **Change tracking** — opt-in via `TrackedBase`: `IsChanged`, `GetChanges`, `Rollback` with byte-level snapshots
- **Soft delete** — embed `SoftBase` instead of `Base`, automatic query filtering, `HardDelete` for permanent removal
- **Optimistic concurrency** — revision-based conflict detection with `ErrRevisionConflict`
- **Transactions** — `RunInTransaction` with panic-safe rollback
- **Migrations** — registry-based, each migration runs atomically in a transaction
- **Struct tag validation** — optional `validate:"required,email"` tags via `go-playground/validator`, enabled with `validate.WithValidation()`
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

    // Insert
    p := &Product{Name: "Widget", Price: 9.99}
    if err := den.Insert(ctx, db, p); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Inserted: %s (ID: %s)\n", p.Name, p.ID)

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

db, err := den.OpenURL("postgres://user:pass@localhost/mydb")
```

## Architecture

```
den/
├── den.go, crud.go, queryset.go    Core API: Open, CRUD, QuerySet
├── iter.go                         Iter() — iter.Seq2 for range loops
├── aggregate.go                    Avg, Sum, Min, Max, GroupBy, Project
├── link.go, backlinks.go           Link[T] relations, BackLinks
├── search.go                       Full-text search (FTSProvider)
├── track.go                        Change tracking: IsChanged, GetChanges
├── soft_delete.go                  Soft delete, HardDelete
├── hooks.go                        Lifecycle hook interfaces
├── revision.go                     Optimistic concurrency
├── tx.go                           Transactions
├── document/                       Base, TrackedBase, SoftBase, TrackedSoftBase
├── where/                          Query condition builders
├── backend/
│   ├── sqlite/                     SQLite backend (pure Go, no CGO)
│   └── postgres/                   PostgreSQL backend (pgx)
├── validate/                       Optional struct tag validation
├── migrate/                        Migration framework
└── dentest/                        Test helpers
```

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

All documents embed one of the base types from the `document` package:

| Base Type | Use Case |
|---|---|
| `document.Base` | Standard documents |
| `document.TrackedBase` | Documents with change tracking |
| `document.SoftBase` | Documents with soft delete |
| `document.TrackedSoftBase` | Both change tracking and soft delete |

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

Den supports automatic struct tag validation via [`go-playground/validator`](https://github.com/go-playground/validator). Enable it as an option when opening the database:

```go
import "github.com/oliverandrich/den/validate"

db, err := den.OpenURL("sqlite:///data.db", validate.WithValidation())
```

Then add `validate` tags to your document structs:

```go
type User struct {
    document.Base
    Name  string `json:"name"  den:"unique" validate:"required,min=3,max=50"`
    Email string `json:"email" den:"unique" validate:"required,email"`
    Age   int    `json:"age"                validate:"gte=0,lte=130"`
}
```

Validation runs automatically before every insert and update. Errors wrap `den.ErrValidation` and can be inspected for field-level detail:

```go
err := den.Insert(ctx, db, &User{Name: "ab"})
if errors.Is(err, den.ErrValidation) {
    var ve *validate.Errors
    if errors.As(err, &ve) {
        for _, fe := range ve.Fields {
            fmt.Printf("%s failed on %s\n", fe.Field, fe.Tag)
        }
    }
}
```

Tag validation and the `Validator` interface coexist — tag validation runs first (structural rules), then `Validate()` (business logic). Without `validate.WithValidation()`, no tag validation occurs (fully backward compatible).

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

## Development

Den uses [just](https://github.com/casey/just) as command runner:

```bash
just setup      # Check that all required dev tools are installed
just test       # Run all tests (SQLite only)
just test-all   # Run all tests including PostgreSQL
just lint       # Run golangci-lint
just fmt        # Format all Go files
just coverage   # Run tests with coverage report
just vuln       # Run vulnerability check
just tidy       # Tidy module dependencies
just beans      # List active beans (issue tracker)
```

Requires Go 1.25+. Run `just setup` to verify your dev environment.

## Dependencies

| Dependency | Purpose |
|---|---|
| `github.com/oklog/ulid/v2` | ULID-based document IDs |
| `github.com/goccy/go-json` | Fast JSON encoding |
| `modernc.org/sqlite` | SQLite backend (pure Go, no CGO) |
| `github.com/jackc/pgx/v5` | PostgreSQL backend |
| `github.com/go-playground/validator/v10` | Struct tag validation (optional, via `den/validate`) |

## License

Den is licensed under the [MIT License](LICENSE).

The Go Gopher was originally designed by [Renee French](https://reneefrench.blogspot.com/) and is licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).
