# Mental Model

A short orientation before the [Quick Start](quickstart.md). Five sentences and you have the right shape in your head for everything that follows.

## Documents and collections

A **document** is a Go struct you define. Den serializes it to JSON (via the standard `encoding/json` rules) and stores it in a SQL table called a **collection** — one collection per document type, one row per document. The actual storage is a JSONB column plus a small set of metadata columns Den manages, so you get the schema-flexibility of a document store with the durability and tooling of SQLite or PostgreSQL underneath.

```go
type Note struct {
    document.Base                       // ID, CreatedAt, UpdatedAt, Rev — required
    Title string `json:"title"`         // serialized as the "title" key
    Body  string `json:"body"`
}
```

`ID` is a 26-character ULID auto-generated on first save; `CreatedAt` and `UpdatedAt` are stamped automatically; `Rev` stays empty unless you opt your type into [revision tracking](../guide/revision-control.md) for optimistic-concurrency conflicts.

## Save: one verb for insert and update

There is no separate `Insert` and `Update` at the top level — `den.Save(ctx, db, doc)` looks at the document's ID and branches: empty ID → insert (a ULID is generated, `BeforeInsert` hooks fire), non-empty ID → update (revision check, `BeforeUpdate` hooks). The same rule applies to `SaveAll` for batches. Read-modify-write becomes `FindByID` → mutate → `Save`. For atomic single-field updates without the read, use `NewQuery[T](db, …).UpdateOne(ctx, fields)` — see [CRUD Operations](../guide/crud.md).

## Registration

Before any operation, every type must be **registered** once with the DB. Registration creates the collection (table) and any secondary indexes the struct's `den:` tags request. It's idempotent — safe to call on every startup; missing tables get created, existing ones are left alone.

```go
db, _ := den.OpenURL(ctx, "sqlite:///notes.db", den.WithTypes(&Note{}))
// or, after Open:
den.Register(ctx, db, &Note{})
```

If you query an unregistered type, the operation returns `ErrNotRegistered` with a message that names the type and tells you which `Register` call to add. There is no auto-discovery — explicit registration is the Go-idiomatic choice.

`document.Base` reserves a small set of underscore-prefixed JSON keys (`_id`, `_created_at`, `_updated_at`, `_rev`) for its standard fields, with `document.SoftDelete` adding `_deleted_at` and friends. The Go-side fields keep natural names (`doc.ID`, `doc.CreatedAt`); the underscore form only appears when you reference the field by JSON name in `where.Field`, `Sort`, or `SetFields`. Use the [`den.FieldID`, `den.FieldCreatedAt`, …](../reference/struct-tags.md#reserved-json-field-names) constants instead of typing the strings — refactor-safe and IDE-discoverable.

## Two struct tags

| Tag | Job |
|---|---|
| `json` | Serialization. Sets the field's key in JSONB. Standard Go semantics. |
| `den` | Den-specific metadata. Indexes, uniqueness, full-text search, omitempty — never a field name. The `omitempty` on the `den` tag controls index behavior (skip the index when the field is zero), not JSON serialization. |

```go
type Note struct {
    document.Base
    Title string   `json:"title"   den:"index"`         // indexed for fast lookup/sort
    Slug  string   `json:"slug"    den:"unique"`        // unique constraint
    Body  string   `json:"body"    den:"fts"`           // full-text search
    Tags  []string `json:"tags"    den:"index,omitempty"`
}
```

The `den:` tag also appears on `GroupBy().Into()` and `Project()` target structs with a different value set (`count`, `sum:price`, `from:foo.bar`, …). The full inventory lives in [Struct Tags Reference](../reference/struct-tags.md).

## Backends

The same code runs against either SQLite or PostgreSQL. The choice happens at `OpenURL` time:

```go
den.OpenURL(ctx, "sqlite:///notes.db")                   // embedded, single binary
den.OpenURL(ctx, "postgres://user:pass@host/db")         // server-based, scales out
```

Every CRUD, query, transaction, and aggregation works the same on both. Backend-specific features (PostgreSQL GIN indexes, SQLite FTS5) sit behind the same Go API; you don't write SQL.

## What's next

That's the whole mental model. From here:

- [Quick Start](quickstart.md) — define a type, insert, query, iterate
- [Documents](../guide/documents.md) — composable embeds for soft-delete, change tracking, attachments
- [Backends](backends.md) — DSN formats, when to pick which
