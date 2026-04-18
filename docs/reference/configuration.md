# Configuration

How to open a Den database, configure backends, and customize document behavior.

---

## Opening a Database

### OpenURL (recommended)

`OpenURL` takes a `context.Context` as its first argument and a URL-style DSN as the second. Backend packages must be imported for side-effect registration.

```go
import (
    "context"

    "github.com/oliverandrich/den"
    _ "github.com/oliverandrich/den/backend/sqlite"
    _ "github.com/oliverandrich/den/backend/postgres"
)

ctx := context.Background()

// SQLite: file-backed
db, err := den.OpenURL(ctx, "sqlite:///path/to/data.db")

// SQLite: in-memory
db, err := den.OpenURL(ctx, "sqlite://:memory:")

// PostgreSQL
db, err := den.OpenURL(ctx, "postgres://user:pass@localhost:5432/mydb")

// PostgreSQL (alias scheme)
db, err := den.OpenURL(ctx, "postgresql://user:pass@localhost/mydb")
```

> **Important:** Backend packages register themselves via `init()`. You must import the backend package even if you don't reference it directly -- use the blank identifier `_`.

### Options

`OpenURL` accepts functional options:

```go
import "github.com/oliverandrich/den/validate"

db, err := den.OpenURL(ctx, "sqlite:///data.db", validate.WithValidation())
```

| Option | Package | Description |
|---|---|---|
| `validate.WithValidation()` | `den/validate` | Enable struct tag validation using `go-playground/validator` |

---

## DSN Formats

| Backend | Scheme | Format | Example |
|---|---|---|---|
| SQLite (file) | `sqlite` | `sqlite:///path/to/file.db` | `sqlite:///data/myapp.db` |
| SQLite (memory) | `sqlite` | `sqlite://:memory:` | `sqlite://:memory:` |
| PostgreSQL | `postgres` | `postgres://user:pass@host:port/dbname` | `postgres://admin:secret@localhost:5432/myapp` |
| PostgreSQL | `postgresql` | `postgresql://user:pass@host:port/dbname` | `postgresql://admin:secret@db.example.com/myapp` |

---

## Registering Document Types

All document types must be registered before use. Registration creates the collection table and indexes.

```go
err := den.Register(ctx, db,
    &Product{},
    &Category{},
    &User{},
)
```

Registration performs:

1. Reflection-based analysis of the struct (fields, types, tags)
2. Collection table creation (or verification if it already exists)
3. Index creation based on `den` struct tags (additive -- new indexes are created, existing ones are kept)

---

## DenSettings

Document types can customize their behavior by implementing the `DenSettable` interface:

```go
type DenSettable interface {
    DenSettings() den.Settings
}
```

### Settings Fields

| Field | Type | Default | Description |
|---|---|---|---|
| `CollectionName` | `string` | lowercase struct name | Override the auto-derived collection name |
| `OmitEmpty` | `bool` | `false` | When true, zero-value fields are omitted from storage by default |
| `UseRevision` | `bool` | `false` | Enable optimistic concurrency control via `_rev` field |
| `NestingDepthPerField` | `map[string]int` | `nil` | Per-field override for link nesting depth |
| `Indexes` | `[]IndexDefinition` | `nil` | Additional indexes (for compound indexes not expressible via struct tags) |

### Example

```go
type Product struct {
    document.Base
    Name     string  `json:"name"  den:"index"`
    Category string  `json:"category" den:"index"`
    Price    float64 `json:"price" den:"index"`
}

func (p Product) DenSettings() den.Settings {
    return den.Settings{
        CollectionName: "products",       // override: default would be "product"
        UseRevision:    true,             // enable optimistic concurrency
        NestingDepthPerField: map[string]int{
            "category": 1,                // shallow fetch for category links
        },
        Indexes: []den.IndexDefinition{
            {
                Name:   "idx_category_price",
                Fields: []string{"category", "price"},  // compound index
            },
        },
    }
}
```

---

## Collection Naming

By default, Den derives the collection name from the struct name:

- Lowercase, no pluralization
- `Product` becomes `product`
- `BlogPost` becomes `blogpost`

Override via `DenSettings().CollectionName`:

```go
func (p Product) DenSettings() den.Settings {
    return den.Settings{CollectionName: "products"}
}
```

---

## Index Definition

Indexes can be defined in two ways:

### Via Struct Tags

```go
type Product struct {
    document.Base
    Name  string  `json:"name"  den:"index"`   // secondary index
    SKU   string  `json:"sku"   den:"unique"`  // unique index
    Body  string  `json:"body"  den:"fts"`     // full-text search index
}
```

### Via Settings (compound indexes)

```go
func (p Product) DenSettings() den.Settings {
    return den.Settings{
        Indexes: []den.IndexDefinition{
            {Name: "idx_category_price", Fields: []string{"category", "price"}},
        },
    }
}
```

Both approaches are additive. Struct tag indexes and Settings indexes are created together during registration.
