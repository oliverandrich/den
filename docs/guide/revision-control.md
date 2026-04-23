# Revision Control

Revision control provides optimistic concurrency, preventing silent data loss when multiple processes update the same document concurrently.

## Enabling Revision Control

Implement `DenSettings()` on your document type with `UseRevision: true`:

```go
type Product struct {
    document.Base
    Name  string  `json:"name"  den:"index"`
    Price float64 `json:"price" den:"index"`
}

func (p Product) DenSettings() den.Settings {
    return den.Settings{UseRevision: true}
}
```

## How It Works

When revision control is enabled:

1. Each document stores a `_rev` field (a random string, regenerated on every write)
2. On `Update`, Den checks that the document's `_rev` matches what is currently stored in the backend
3. If the revision does not match (another process updated the document since you read it), `den.ErrRevisionConflict` is returned
4. The check-and-write happens atomically within a single backend transaction

```go
p, _ := den.FindByID[Product](ctx, db, "prod_001")
p.Price = 29.99

// If someone else updated this document since we read it:
err := den.Update(ctx, db, p)
// err == den.ErrRevisionConflict
```

## Handling Conflicts

A typical conflict-handling pattern is to re-read and retry:

```go
p, _ := den.FindByID[Product](ctx, db, "prod_001")
p.Price = 29.99

err := den.Update(ctx, db, p)
if errors.Is(err, den.ErrRevisionConflict) {
    // Re-read the latest version and decide how to proceed
    latest, _ := den.FindByID[Product](ctx, db, p.ID)
    latest.Price = 29.99
    err = den.Update(ctx, db, latest)
}
```

## Force-Writing

Use `den.IgnoreRevision()` to bypass the revision check and force-write:

```go
err := den.Update(ctx, db, p, den.IgnoreRevision())
```

!!! warning
    `IgnoreRevision()` overwrites the document regardless of concurrent modifications. Use it only when you intentionally want to discard any changes made by other processes.

## Pessimistic Locking with Transactions

Revision control is *optimistic* — it detects conflicts after the fact. When you need to *prevent* conflicts entirely, combine transactions with revision control for a pessimistic locking pattern:

```go
err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    // Read the document within the transaction.
    // SQLite: the IMMEDIATE transaction already holds an exclusive write lock.
    // PostgreSQL: the transaction provides a consistent snapshot.
    p, err := den.FindByID[Product](ctx, tx, id)
    if err != nil {
        return err
    }

    p.Price = 29.99

    // The revision check ensures no one modified the document
    // between our read and this write.
    return den.Update(ctx, tx, p)
})
```

If the revision check fails, the transaction rolls back and you can retry:

```go
for range 3 {
    err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
        p, err := den.FindByID[Product](ctx, tx, id)
        if err != nil {
            return err
        }
        p.Price = 29.99
        return den.Update(ctx, tx, p)
    })
    if !errors.Is(err, den.ErrRevisionConflict) {
        break
    }
}
```

!!! tip
    On SQLite, transactions use `_txlock(immediate)`, which serializes all writers. This means the read-modify-write cycle above is already exclusive — revision conflicts are unlikely but still caught as a safety net. On PostgreSQL (READ COMMITTED), concurrent transactions can modify the same row, making the revision check essential.

## When to Use Revision Control

Revision control is useful when:

- Multiple users or processes can modify the same document
- Silent data loss from concurrent writes is unacceptable
- You need application-level conflict detection independent of the backend's transaction isolation

!!! note
    Revision control is orthogonal to transactions. Transactions provide isolation at the database level. Revision control provides conflict detection at the application level, across separate request cycles.

## Interaction with Soft Delete

`Delete` on a document that opts into both `SoftDelete` and `UseRevision` participates in the revision chain: the stored `_rev` is verified, a fresh `_rev` is assigned, and the write is atomic. A concurrent writer holding the pre-delete revision therefore sees `ErrRevisionConflict` on its next `Update` instead of silently clobbering `DeletedAt`. `IgnoreRevision()` opts out; `HardDelete()` removes the row outright and is not subject to the check. See [Soft Delete](soft-delete.md#combining-with-revision-control) for the full pattern.
