# Struct Tags

Den uses three struct tags: `json` for serialization, `den` for Den-specific metadata, and an optional `validate` tag for `go-playground/validator` integration. The `den` tag carries different sets of values depending on **which struct it appears on** — a document type, a `GroupBy().Into()` target, or a `Project()` target.

---

## Tag Overview

| Tag | Purpose | Example |
|---|---|---|
| `json` | Sets the serialized field name (the key stored in JSONB) | `json:"name"` |
| `den` | Den-specific metadata (index/unique/fts on documents; `from:` on projections; `count`/`sum:`/`group_key` on aggregations) | `den:"index"` |
| `validate` | Struct tag validation rules (requires `validate.WithValidation()`) | `validate:"required,email"` |

---

## All `den:` Values by Context

Every supported `den:` value, where it's valid, and what it does. Values are context-specific: an aggregation tag on a document field is rejected at registration; a document tag on an aggregation target is ignored.

| `den:` value | Valid on | Meaning |
|---|---|---|
| `index` | Document field | Secondary index for lookups and sorts |
| `unique` | Document field | Unique index (doubles as a lookup index — `index` is redundant alongside) |
| `fts` | Document field | Include this field in full-text search |
| `omitempty` | Document field | Omit from storage when zero-valued |
| `unique_together:GROUP` | Document field | Composite unique index keyed by `GROUP` (multiple fields with the same group form one index) |
| `index_together:GROUP` | Document field | Composite (non-unique) index keyed by `GROUP` |
| `from:path.to.field` | `Project()` target | Extract a nested value from the source document |
| `group_key` | `GroupBy().Into()` target | Receives the single group key value |
| `group_key:N` | `GroupBy().Into()` target | Positional group key (slot `N`, zero-indexed) for multi-field `GroupBy(field0, field1, …)` |
| `count` | `GroupBy().Into()` target | Count of documents per group |
| `avg:FIELD` | `GroupBy().Into()` target | Average of `FIELD` per group |
| `sum:FIELD` | `GroupBy().Into()` target | Sum of `FIELD` per group |
| `min:FIELD` | `GroupBy().Into()` target | Minimum of `FIELD` per group |
| `max:FIELD` | `GroupBy().Into()` target | Maximum of `FIELD` per group |

Multiple values combine with commas: `den:"index,omitempty"`. The combinations that make sense are document-field × document-field; mixing across contexts is rejected (document fields refuse `from:`, projection targets refuse `index`, etc.).

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
| `den:"group_key"` | Receives the group key value (single-field `GroupBy`) |
| `den:"group_key:N"` | Positional group key (slot `N`, zero-indexed) for multi-field `GroupBy` |
| `den:"avg:fieldname"` | Average of `fieldname` within the group |
| `den:"sum:fieldname"` | Sum of `fieldname` within the group |
| `den:"min:fieldname"` | Minimum of `fieldname` within the group |
| `den:"max:fieldname"` | Maximum of `fieldname` within the group |
| `den:"count"` | Number of documents in the group |

Two struct fields claiming the same `group_key:N` slot, or two fields carrying the same aggregate tag (e.g. two `den:"sum:price"`), are rejected at the `Into` call — the framework refuses ambiguous targets.

### Single-key example

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

### Multi-key example

```go
type RegionStats struct {
    Category string  `den:"group_key:0"` // first GroupBy field
    Region   string  `den:"group_key:1"` // second GroupBy field
    Count    int64   `den:"count"`
    Total    float64 `den:"sum:price"`
}

var stats []RegionStats
err := den.NewQuery[Product](db).GroupBy("category", "region").Into(ctx, &stats)
```

The slot index in `group_key:N` matches the position in the `GroupBy(...)` argument list. Unindexed `den:"group_key"` is shorthand for `group_key:0` and is only valid when `GroupBy` was called with exactly one field.

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
| `document.SoftDelete` | Opt-in. DeletedAt, DeletedBy, DeleteReason |
| `document.Tracked` | Opt-in. Snapshot machinery for IsChanged, GetChanges, Revert |

Compose freely: `struct { document.Base; document.SoftDelete; document.Tracked; ... }`.

---

## Reserved JSON Field Names

The standard embeds (`document.Base` and `document.SoftDelete`) install JSON keys with an underscore prefix to namespace them from user-defined fields — the same convention MongoDB uses. Whenever you need one of these in code that takes a string (`where.Field`, `Sort`, `SetFields`, `After` / `Before`, `Project`'s `den:"from:…"`) prefer the constants exported from the `den` package over the literal — typos become compile errors and a rename stays safe across the codebase.

| Constant | Value | Comes from | When to query it |
|---|---|---|---|
| `den.FieldID` | `_id` | `document.Base.ID` | Lookup-by-id, cursor pagination, sort by insert order (ULIDs sort chronologically) |
| `den.FieldCreatedAt` | `_created_at` | `document.Base.CreatedAt` | Time-window filters, "newest first" sorts |
| `den.FieldUpdatedAt` | `_updated_at` | `document.Base.UpdatedAt` | "Last touched" filters, change detection |
| `den.FieldRev` | `_rev` | `document.Base.Rev` | Rare — usually accessed via `IgnoreRevision()` instead of a manual where |
| `den.FieldDeletedAt` | `_deleted_at` | `document.SoftDelete.DeletedAt` | Soft-deleted-only queries (combine with `IncludeDeleted` + `IsNotNil`) |
| `den.FieldDeletedBy` | `_deleted_by` | `document.SoftDelete.DeletedBy` | Per-actor audit queries on soft-deleted rows |
| `den.FieldDeleteReason` | `_delete_reason` | `document.SoftDelete.DeleteReason` | Per-reason audit queries on soft-deleted rows |

The Go-side fields keep their natural names (`doc.ID`, `doc.CreatedAt`, …). Only the JSON tag (and therefore the SQL JSONB access path) uses the underscore form. Storage is independent of the constants — renaming the JSON tag would be a breaking storage change, not a source rename.

```go
// Cursor pagination over the natural ULID order:
page, _ := den.NewQuery[Post](db).
    Sort(den.FieldID, den.Asc).
    Limit(50).All(ctx)

// Recently touched, oldest first:
recent, _ := den.NewQuery[Post](db,
    where.Field(den.FieldUpdatedAt).Gt(cutoff),
).Sort(den.FieldUpdatedAt, den.Asc).All(ctx)
```
