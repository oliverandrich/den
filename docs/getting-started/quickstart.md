# Quick Start

This guide walks through a complete working example: defining a document, storing it, and querying it back.

## Create a Project

```bash
mkdir myapp && cd myapp
go mod init myapp
go get github.com/oliverandrich/den@latest
```

## Define a Document

Every document struct embeds `document.Base`, which provides `ID`, `Rev`, `CreatedAt`, and `UpdatedAt` fields. Use `json` tags for field names and `den` tags for index metadata.

```go
type Product struct {
    document.Base
    Name  string  `json:"name"  den:"index"`
    Price float64 `json:"price" den:"index"`
}
```

## Full Example

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

    // Register document types — creates collections and indexes
    if err := den.Register(ctx, db, &Product{}); err != nil {
        log.Fatal(err)
    }

    // Insert a document
    p := &Product{Name: "Widget", Price: 9.99}
    if err := den.Insert(ctx, db, p); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Inserted: %s (ID: %s)\n", p.Name, p.ID)

    // Query with conditions
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

## Switching to PostgreSQL

Change the import and the DSN — the rest of your code stays identical.

```go
import _ "github.com/oliverandrich/den/backend/postgres" // instead of sqlite

db, err := den.OpenURL(ctx, "postgres://user:pass@localhost/mydb")
```

!!! tip "Same API, different engine"
    Every Den operation — `Insert`, `NewQuery`, `Update`, `Delete`, `RunInTransaction` — works the same on both backends. Choose SQLite for embedded single-binary deployments and PostgreSQL when you need replication or scale.

## Next Steps

- [Backends](backends.md) — DSN formats, comparison, and when to use which
- [Documents](../guide/documents.md) — Base types, struct tags, and lifecycle hooks
- [API Reference](../reference/api.md) — Complete API overview
