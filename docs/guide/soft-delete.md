# Soft Delete

## Enabling Soft Delete

Embed `document.SoftBase` instead of `document.Base` to opt into soft delete:

```go
import "github.com/oliverandrich/den/document"

type Product struct {
    document.SoftBase
    Name  string  `json:"name"  den:"index"`
    Price float64 `json:"price" den:"index"`
}
```

`SoftBase` extends `Base` with a `DeletedAt` timestamp:

```go
type SoftBase struct {
    Base
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
products, _ := den.NewQuery[Product](ctx, db).All()
```

## Including Deleted Documents

Use `IncludeDeleted()` to bypass the automatic filter:

```go
all, _ := den.NewQuery[Product](ctx, db).IncludeDeleted().All()
```

## Permanent Removal

Use `HardDelete` to permanently remove a document from storage:

```go
err := den.HardDelete(ctx, db, product)
```

!!! warning
    `HardDelete` is irreversible. The document is permanently removed from the backend — there is no way to recover it.

## Checking Delete Status

`SoftBase` provides an `IsDeleted()` helper:

```go
if product.IsDeleted() {
    fmt.Println("Product was deleted at", product.DeletedAt)
}
```

## Combining with Change Tracking

For documents that need both soft delete and change tracking, embed `document.TrackedSoftBase`:

```go
type AuditLog struct {
    document.TrackedSoftBase
    Action string `json:"action"`
    Detail string `json:"detail"`
}
```

This gives you `IsChanged`, `GetChanges`, and `Rollback` alongside soft delete behavior.

!!! tip
    Choose your base type based on the features you need. See the [Documents](documents.md) guide for the full base type matrix.

## How It Works

When Den detects that a document type embeds `SoftBase` (via reflection at registration time), it:

1. Rewrites `Delete()` to set `DeletedAt = time.Now()` and update the document instead of removing the row
2. Injects an automatic `where.Field("_deleted_at").IsNil()` condition into all queries for that collection
3. Provides `HardDelete()` for actual permanent deletion
4. Provides `IncludeDeleted()` to bypass the automatic filter
