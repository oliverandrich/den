# Soft Delete

## Enabling Soft Delete

Embed `document.SoftDelete` alongside `document.Base` to opt into soft delete:

```go
import "github.com/oliverandrich/den/document"

type Product struct {
    document.Base
    document.SoftDelete
    Name  string  `json:"name"  den:"index"`
    Price float64 `json:"price" den:"index"`
}
```

`SoftDelete` is a tiny composable mixin that adds a `DeletedAt` timestamp:

```go
type SoftDelete struct {
    DeletedAt *time.Time `json:"_deleted_at,omitempty"`
}
```

## Behavior

When you call `den.Delete` on a soft-delete document, Den sets `DeletedAt` to the current time instead of removing the row from storage:

```go
err := den.Delete(ctx, db, product)
product.IsDeleted() // true
```

All standard queries automatically exclude soft-deleted documents:

```go
// Returns only non-deleted products
products, _ := den.NewQuery[Product](db).All(ctx)
```

## Including Deleted Documents

Use `IncludeDeleted()` to bypass the automatic filter:

```go
all, _ := den.NewQuery[Product](db).IncludeDeleted().All(ctx)
```

## Permanent Removal

Pass `HardDelete()` as a `CRUDOption` to `Delete` to permanently remove a document from storage:

```go
err := den.Delete(ctx, db, product, den.HardDelete())
```

`HardDelete()` composes with other CRUDOptions, so you can combine it with things like `WithLinkRule(LinkDelete)`.

!!! warning
    `HardDelete()` is irreversible. The document is permanently removed from the backend — there is no way to recover it.

## Checking Delete Status

`SoftDelete` provides an `IsDeleted()` helper:

```go
if product.IsDeleted() {
    fmt.Println("Product was deleted at", product.DeletedAt)
}
```

## Combining with Revision Control

When a soft-delete document also opts into [revision control](revision-control.md) (`UseRevision: true`), soft-delete participates in the revision chain exactly like `Update`:

- `Delete` verifies the stored `_rev` against the in-memory value, assigns a fresh `_rev`, and writes atomically.
- A concurrent writer holding the pre-delete revision sees `ErrRevisionConflict` on its next `Update` — it cannot silently clobber `DeletedAt`.
- `IgnoreRevision()` composes with `Delete`, so callers can deliberately bypass the check when needed.

```go
type Article struct {
    document.Base
    document.SoftDelete
    Title string `json:"title"`
}

func (a Article) DenSettings() den.Settings {
    return den.Settings{UseRevision: true}
}

// Both goroutines loaded the same _rev.
_ = den.Delete(ctx, db, a) // bumps _rev, records DeletedAt

b.Title = "stale update"
err := den.Update(ctx, db, b)
// err == den.ErrRevisionConflict — b held the pre-delete revision
```

`HardDelete()` physically removes the row and is not subject to revision checks.

## Combining with Change Tracking

`SoftDelete` and `Tracked` are independent embeds — compose them freely:

```go
type AuditLog struct {
    document.Base
    document.SoftDelete
    document.Tracked
    Action string `json:"action"`
    Detail string `json:"detail"`
}
```

This gives you `IsChanged`, `GetChanges`, and `Revert` alongside soft delete behavior.

!!! tip
    See the [Documents](documents.md) guide for the full list of composable embeds and example compositions.

## How It Works

When Den detects that a document type has a `_deleted_at` JSON field (via reflection at registration time — regardless of whether it comes from the `SoftDelete` embed or a hand-rolled field), it:

1. Rewrites `Delete()` to set `DeletedAt = time.Now()` and update the document instead of removing the row
2. Injects an automatic `where.Field("_deleted_at").IsNil()` condition into all queries for that collection
3. Provides `HardDelete()` for actual permanent deletion
4. Provides `IncludeDeleted()` to bypass the automatic filter
