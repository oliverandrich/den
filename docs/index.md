# Den

<p align="center"><img src="assets/cover.jpg" alt="Go gophers organizing documents in their den"></p>

<p align="center"><em>"Every <a href="https://github.com/oliverandrich/burrow">burrow</a> needs a den — a place to store what matters and find it again when you need it."</em></p>

An ODM for Go with two storage backends — SQLite and PostgreSQL. Same API, your choice of engine.

## Features

- **Two backends, one API** — SQLite (embedded, pure Go, no CGO) and PostgreSQL (server-based, JSONB + GIN indexes)
- **Chainable QuerySet** — fluent builder with lazy evaluation: `Where`, `Sort`, `Limit`, `Skip`, `All`, `First`, `Count`
- **Range iteration** — `Iter()` returns `iter.Seq2[*T, error]` for memory-efficient streaming
- **Typed relations** — `Link[T]` and `[]Link[T]` with cascade write/delete and eager/lazy fetch
- **Full-text search** — FTS5 (SQLite), tsvector (PostgreSQL), unified `Search()` API
- **Lifecycle hooks** — `BeforeInsert`, `AfterUpdate`, `Validate`, and more via interfaces
- **Change tracking** — opt-in `TrackedBase` with `IsChanged`, `GetChanges`, `Rollback`
- **Soft delete** — embed `SoftBase`, automatic query filtering, `HardDelete` for permanent removal
- **Optimistic concurrency** — revision-based conflict detection
- **Transactions** — `RunInTransaction` with panic-safe rollback
- **Migrations** — registry-based, each migration runs atomically
- **Struct tag validation** — optional `validate` tags via `go-playground/validator`

## Quick Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/oliverandrich/den"
    _ "github.com/oliverandrich/den/backend/sqlite"
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

    db, err := den.OpenURL("sqlite:///products.db")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    den.Register(ctx, db, &Product{})
    den.Insert(ctx, db, &Product{Name: "Widget", Price: 9.99})

    products, _ := den.NewQuery[Product](ctx, db,
        where.Field("price").Lt(20.0),
    ).Sort("name", den.Asc).All()

    for _, p := range products {
        fmt.Printf("%s — $%.2f\n", p.Name, p.Price)
    }
}
```

## Quick Links

<div class="grid cards" markdown>

- [:material-download: **Installation**](getting-started/installation.md) — Get Den into your project
- [:material-rocket-launch: **Quick Start**](getting-started/quickstart.md) — Build your first app
- [:material-file-document: **Documents**](guide/documents.md) — Struct embedding, tags, and base types
- [:material-book-open-variant: **API Reference**](reference/api.md) — Complete API overview

</div>
