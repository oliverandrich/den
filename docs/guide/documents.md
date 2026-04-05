# Documents

## Base Types

Every Den document embeds one of four base types from the `document` package. Choose based on the features you need:

| Base Type | Change Tracking | Soft Delete | Use Case |
|---|---|---|---|
| `document.Base` | No | No | Simple documents without audit needs |
| `document.TrackedBase` | Yes | No | Documents where you need `IsChanged`, `GetChanges`, `Rollback` |
| `document.SoftBase` | No | Yes | Documents that should never be permanently deleted by default |
| `document.TrackedSoftBase` | Yes | Yes | Full audit trail with recoverability |

```go
package document

type Base struct {
    ID        string    `json:"_id"`
    CreatedAt time.Time `json:"_created_at"`
    UpdatedAt time.Time `json:"_updated_at"`
    Rev       string    `json:"_rev,omitempty"` // populated when UseRevision is enabled
}

type TrackedBase struct {
    Base
    snapshot []byte // internal, not serialized
}

type SoftBase struct {
    Base
    DeletedAt *time.Time `json:"_deleted_at,omitempty"`
}

type TrackedSoftBase struct {
    SoftBase
    snapshot []byte // internal, not serialized
}
```

## Struct Tag Syntax

Den uses two struct tags with distinct responsibilities:

- **`json`** -- Sets the serialized field name (the key stored in JSONB). Standard Go `encoding/json` rules apply.
- **`den`** -- Carries Den-specific metadata only. No field name, just options.

Available `den` tag options:

| Option | Effect |
|---|---|
| `index` | Creates a secondary index on this field |
| `unique` | Creates a unique index on this field |
| `fts` | Includes this field in full-text search |
| `omitempty` | Omits the field from storage when it has a zero value |

```go
type Product struct {
    document.Base
    Name  string   `json:"name"  den:"index"`
    SKU   string   `json:"sku"   den:"unique"`
    Price float64  `json:"price" den:"index"`
    Body  string   `json:"body"  den:"fts"`
    Tags  []string `json:"tags"  den:"index,omitempty"`
}
```

!!! note
    The `json` tag controls serialization -- it determines the key name in the stored JSONB document. The `den` tag never contains a field name; it only carries index, unique, fts, and omitempty options.

## Collection Naming

Den derives collection names automatically: lowercase struct name, no pluralization.

| Struct | Collection Name |
|---|---|
| `Product` | `product` |
| `Category` | `category` |
| `AuditLog` | `auditlog` |

Override the default by implementing `DenSettings()`:

```go
func (p Product) DenSettings() den.Settings {
    return den.Settings{
        CollectionName: "products",
    }
}
```

## ID Generation

Den uses [ULID](https://github.com/oklog/ulid) for document IDs -- lexicographically sortable, timestamp-ordered, 26 characters.

```go
p := &Product{Name: "Widget", Price: 9.99}
den.Insert(ctx, db, p)
fmt.Println(p.ID) // "01HQ3K8V2X..."
```

ULIDs are preferred over UUIDs because they sort chronologically, which benefits B-tree indexes in both SQLite and PostgreSQL -- improving write and scan performance.

You can set an ID manually before insert. If `ID` is empty, Den generates a ULID automatically:

```go
p := &Product{Name: "Widget"}
p.ID = "my-custom-id"
den.Insert(ctx, db, p) // uses "my-custom-id"
```

## DenSettings Interface

Implement `DenSettings()` on your document struct to configure collection-level behavior:

```go
type DenSettable interface {
    DenSettings() den.Settings
}

type Settings struct {
    CollectionName       string            // override auto-derived name
    OmitEmpty            bool              // omit zero-value fields by default
    UseRevision          bool              // enable optimistic concurrency control
    NestingDepthPerField map[string]int    // per-field nesting depth overrides
    Indexes              []IndexDefinition // compound indexes
}
```

Example with multiple settings:

```go
func (p Product) DenSettings() den.Settings {
    return den.Settings{
        CollectionName: "products",
        UseRevision:    true,
        OmitEmpty:      true,
        Indexes: []den.IndexDefinition{
            {Name: "idx_category_price", Fields: []string{"category", "price"}},
        },
    }
}
```

## Index Definitions

### Via Struct Tags

Single-field indexes are defined directly on the struct:

```go
type Product struct {
    document.Base
    Name  string  `json:"name"  den:"index"`   // secondary index
    SKU   string  `json:"sku"   den:"unique"`  // unique index
    Price float64 `json:"price" den:"index"`   // secondary index
}
```

=== "SQLite"

    ```sql
    CREATE INDEX IF NOT EXISTS idx_product_name
        ON product(json_extract(data, '$.name'));

    CREATE UNIQUE INDEX IF NOT EXISTS idx_product_sku
        ON product(json_extract(data, '$.sku'));
    ```

=== "PostgreSQL"

    ```sql
    CREATE INDEX IF NOT EXISTS idx_product_name
        ON product(((data->>'name')));

    CREATE UNIQUE INDEX IF NOT EXISTS idx_product_sku
        ON product(((data->>'sku')));
    ```

### Via Settings (Compound Indexes)

For multi-field indexes, use `DenSettings()`:

```go
func (p Product) DenSettings() den.Settings {
    return den.Settings{
        Indexes: []den.IndexDefinition{
            {Name: "idx_category_price", Fields: []string{"category", "price"}},
        },
    }
}
```

!!! tip
    Struct tag indexes are best for single-field indexes. Use `DenSettings().Indexes` when you need compound indexes spanning multiple fields.

## Nullable Unique Constraints

When a pointer field is tagged with `den:"unique"`, Den creates a partial unique index. Uniqueness is only enforced for non-nil values -- multiple documents can have `nil` for that field.

```go
type User struct {
    document.Base
    Username string  `json:"username" den:"unique"`         // always required, always unique
    Email    *string `json:"email,omitempty" den:"unique"`  // optional, unique when set
}

// Both users have nil Email -- no conflict:
den.Insert(ctx, db, &User{Username: "alice"})  // Email: nil
den.Insert(ctx, db, &User{Username: "bob"})    // Email: nil

// But duplicate non-nil values are rejected:
den.Insert(ctx, db, &User{Username: "carol", Email: ptr("carol@example.com")}) // ok
den.Insert(ctx, db, &User{Username: "dave",  Email: ptr("carol@example.com")}) // ErrDuplicate
```

=== "SQLite"

    ```sql
    CREATE UNIQUE INDEX IF NOT EXISTS idx_user_email
        ON user(json_extract(data, '$.email'))
        WHERE json_extract(data, '$.email') IS NOT NULL;
    ```

=== "PostgreSQL"

    ```sql
    CREATE UNIQUE INDEX IF NOT EXISTS idx_user_email
        ON "user"(((data->>'email')))
        WHERE (data->>'email') IS NOT NULL;
    ```

!!! warning
    Nullable unique constraints require pointer fields (`*string`, `*int`, etc.). Value-type fields with `den:"unique"` always enforce uniqueness, including for zero values.
