# Struct Tags

Den uses two struct tags: `json` for serialization and `den` for index and metadata options. An optional `validate` tag integrates with `go-playground/validator`.

---

## Tag Overview

| Tag | Purpose | Example |
|---|---|---|
| `json` | Sets the serialized field name (the key stored in JSONB) | `json:"name"` |
| `den` | Den-specific options: indexing, uniqueness, FTS, omitempty | `den:"index"` |
| `validate` | Struct tag validation rules (requires `validate.WithValidation()`) | `validate:"required,email"` |

---

## The `json` Tag

The `json` tag controls how a field is serialized to JSON. This is the standard Go `encoding/json` tag.

```go
type Product struct {
    document.Base
    Name  string  `json:"name"`
    Price float64 `json:"price"`
    SKU   string  `json:"sku"`
}
```

- The `json` tag value becomes the field's key in the stored JSONB document
- Use `json:"field,omitempty"` to omit zero-value fields from JSON (standard Go behavior)
- Use `json:"-"` to exclude a field from serialization entirely

---

## The `den` Tag

The `den` tag carries Den-specific metadata. It does **not** set the field name -- that is always the `json` tag's job.

### Options

| Option | Description | Example |
|---|---|---|
| `index` | Create a secondary index on this field | `den:"index"` |
| `unique` | Create a unique index on this field | `den:"unique"` |
| `fts` | Include this field in full-text search | `den:"fts"` |
| `omitempty` | Omit this field from storage when zero-valued | `den:"omitempty"` |
| `unique_together:group` | Composite unique index — fields with the same group name | `den:"unique_together:feed_guid"` |
| `index_together:group` | Composite index (non-unique) — fields with the same group name | `den:"index_together:user_date"` |

Options can be combined with commas:

```go
Tags []string `json:"tags" den:"index,omitempty"`
```

!!! note "`unique` already implies a lookup index"
    A unique index is itself a usable lookup index, so `den:"unique"` alone is enough for equality queries — you do not need to combine it with `index`. `den:"index,unique"` is accepted but redundant: Den creates only the unique index and silently drops the plain one.

### Complete Example

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

### Nullable Unique Fields

Pointer fields with `unique` create a nullable unique constraint. Multiple documents can have `nil` for the field without violating the constraint:

```go
type User struct {
    document.Base
    Username string  `json:"username" den:"unique"`        // always required, always unique
    Email    *string `json:"email,omitempty" den:"unique"`  // optional, unique when set
}
```

Both backends implement this as a partial unique index (`WHERE ... IS NOT NULL`).

### Composite Unique Constraints

Use `unique_together` to enforce uniqueness across multiple fields. Fields sharing the same group name form a single composite unique index:

```go
type Entry struct {
    document.Base
    Feed string `json:"feed" den:"unique_together:feed_guid"`
    GUID string `json:"guid" den:"unique_together:feed_guid"`
    Body string `json:"body"`
}
```

The combination `(feed, guid)` must be unique, but individual values can repeat. The group name (`feed_guid`) becomes part of the index name: `idx_entry_feed_guid`.

For non-unique composite indexes, use `index_together`:

```go
type Event struct {
    document.Base
    UserID string `json:"user_id" den:"index_together:user_date"`
    Date   string `json:"date"    den:"index_together:user_date"`
}
```

Composite unique indexes include a `WHERE ... IS NOT NULL` clause for all participating fields, matching the behavior of single-field nullable unique constraints.

---

## The `validate` Tag

When `validate.WithValidation()` is passed as an option to `den.OpenURL` or `den.Open`, Den validates documents using `go-playground/validator` struct tags before insert and update operations.

```go
import "github.com/oliverandrich/den/validate"

db, err := den.OpenURL(ctx, "sqlite:///data.db", validate.WithValidation())
```

```go
type User struct {
    document.Base
    Name  string `json:"name"  validate:"required,min=3,max=50"`
    Email string `json:"email" validate:"required,email"`
    Age   int    `json:"age"   validate:"gte=0,lte=130"`
}
```

Validation errors are returned as `den.ErrValidation` and can be unwrapped for per-field details.

---

## Aggregation Struct Tags

Used with `GroupBy().Into()` to define the shape of aggregation results.

| Tag | Description |
|---|---|
| `den:"group_key"` | Receives the group key value |
| `den:"avg:fieldname"` | Average of `fieldname` within the group |
| `den:"sum:fieldname"` | Sum of `fieldname` within the group |
| `den:"min:fieldname"` | Minimum of `fieldname` within the group |
| `den:"max:fieldname"` | Maximum of `fieldname` within the group |
| `den:"count"` | Number of documents in the group |

### Example

```go
type CategoryStats struct {
    Category string  `den:"group_key"`
    AvgPrice float64 `den:"avg:price"`
    Total    float64 `den:"sum:price"`
    Count    int64   `den:"count"`
    MinPrice float64 `den:"min:price"`
    MaxPrice float64 `den:"max:price"`
}

var results []CategoryStats
err := den.NewQuery[Product](db,
    where.Field("status").Eq("active"),
).GroupBy("category.name").Into(ctx, &results)
```

---

## Projection Struct Tags

Used with `Project()` to select a subset of fields from query results.

Simple projections use `json` tags for field name resolution. For nested field extraction, use the `den:"from:"` tag.

| Tag | Description |
|---|---|
| `json:"fieldname"` | Map to a top-level document field by its JSON key |
| `den:"from:nested.field"` | Extract a value from a nested field path |

### Example

```go
// Simple projection -- uses json tags
type ProductSummary struct {
    Name  string  `json:"name"`
    Price float64 `json:"price"`
}

// Nested field extraction -- uses den:"from:" tag
type ProductView struct {
    Name         string `json:"name"`
    CategoryName string `den:"from:category.name"`
}

var summaries []ProductSummary
err := den.NewQuery[Product](db).Project(ctx, &summaries)

var views []ProductView
err := den.NewQuery[Product](db).Project(ctx, &views)
```

---

## Document Base Types

Composable embeds for feature opt-in:

| Embed | Purpose |
|---|---|
| `document.Base` | Required. ID, CreatedAt, UpdatedAt, Rev |
| `document.SoftDelete` | Opt-in. DeletedAt, IsDeleted |
| `document.Tracked` | Opt-in. Snapshot machinery for IsChanged, GetChanges, Revert |

Compose freely: `struct { document.Base; document.SoftDelete; document.Tracked; ... }`.
