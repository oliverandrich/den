# CRUD Operations

!!! warning
    All document types must be registered with `den.Register()` before any CRUD operation. Attempting to use an unregistered type returns `ErrNotRegistered`.

```go
err := den.Register(ctx, db,
    &Product{},
    &Category{},
)
```

## Insert

```go
p := &Product{Name: "Widget", Price: 9.99}
err := den.Insert(ctx, db, p)
// p.ID is now set (auto-generated ULID if it was empty)
// p.CreatedAt and p.UpdatedAt are set automatically
```

Insert triggers the full lifecycle hook chain: `BeforeInsert` -> `BeforeSave` -> tag validation -> `Validate` -> write -> `AfterInsert` -> `AfterSave`. Mutating hooks run before validation so they can populate defaults. If any hook or validation step returns an error, the insert is aborted.

### InsertMany

Insert multiple documents in a single batch:

```go
products := []*Product{
    {Name: "Widget", Price: 9.99},
    {Name: "Gadget", Price: 19.99},
    {Name: "Doohickey", Price: 4.99},
}
err := den.InsertMany(ctx, db, products)
```

## FindByID / FindByIDs

Direct key lookup -- the fastest query path:

```go
product, err := den.FindByID[Product](ctx, db, "01HQ3K8V2X...")
if errors.Is(err, den.ErrNotFound) {
    // document does not exist
}
```

Fetch multiple documents by their IDs:

```go
ids := []string{"01HQ3K8V2X...", "01HQ3K9A1Y...", "01HQ3KBC3Z..."}
products, err := den.FindByIDs[Product](ctx, db, ids)
```

## Update

Update performs a full document write. The document must have an ID.

```go
product, _ := den.FindByID[Product](ctx, db, "01HQ3K8V2X...")
product.Price = 29.99
err := den.Update(ctx, db, product)
```

When revision control is enabled (`UseRevision: true` in `DenSettings`), Update checks that the document's revision matches the stored version. If another process modified the document since it was read, `ErrRevisionConflict` is returned:

```go
err := den.Update(ctx, db, product)
if errors.Is(err, den.ErrRevisionConflict) {
    // another process modified this document -- re-read and retry
}

// Force-write regardless of revision:
err := den.Update(ctx, db, product, den.IgnoreRevision())
```

## Bulk Update via QuerySet

Update specific fields on all documents matching a query. Returns the number of modified documents:

```go
count, err := den.NewQuery[Product](db,
    where.Field("category").Eq("old"),
).Update(ctx, den.SetFields{"category": "new"})
// count = number of documents updated
```

!!! tip
    Bulk updates are more convenient than loading, modifying, and saving each document individually. The update runs in a single transaction, modifying each matching document individually.

## FindOneAndUpdate

Atomic find-and-modify in a single transaction. Finds the first document matching the conditions, applies the field updates, and returns the modified document.

```go
job, err := den.FindOneAndUpdate[Job](ctx, db,
    den.SetFields{
        "status":     "running",
        "started_at": time.Now(),
    },
    where.Field("status").Eq("pending"),
    where.Field("scheduled_at").Lte(time.Now()),
)
if errors.Is(err, den.ErrNotFound) {
    // no pending jobs available
}
```

This is the idiomatic pattern for implementing a job queue. Multiple workers can call `FindOneAndUpdate` concurrently -- each one atomically claims a different job, preventing double-processing:

```go
// Worker loop
for {
    job, err := den.FindOneAndUpdate[Job](ctx, db,
        den.SetFields{
            "status":    "running",
            "worker_id": workerID,
        },
        where.Field("status").Eq("pending"),
        where.Field("scheduled_at").Lte(time.Now()),
    )
    if errors.Is(err, den.ErrNotFound) {
        time.Sleep(time.Second)
        continue
    }
    if err != nil {
        log.Printf("error claiming job: %v", err)
        continue
    }

    processJob(job)
}
```

## Delete

Delete a specific document:

```go
err := den.Delete(ctx, db, &product)
```

!!! note
    If the document embeds `document.SoftDelete`, `Delete` sets `DeletedAt` instead of removing the document from storage. Pass `den.HardDelete()` to permanently remove a soft-deleted document.

```go
// Soft-delete (sets DeletedAt, document remains in storage)
err := den.Delete(ctx, db, &product)

// Permanent removal
err := den.Delete(ctx, db, &product, den.HardDelete())
```

### DeleteMany

Delete all documents matching conditions. Returns the number of deleted documents:

```go
count, err := den.DeleteMany[Product](ctx, db,
    []where.Condition{where.Field("status").Eq("archived")},
)
```

With link cascade -- delete the documents and all their linked documents:

```go
count, err := den.DeleteMany[Product](ctx, db,
    []where.Condition{where.Field("status").Eq("archived")},
    den.WithLinkRule(den.LinkDelete),
)
```

## Refresh

Re-read a document from the database, replacing all field values with the current stored state. Useful when another goroutine or process may have modified the document:

```go
product, _ := den.FindByID[Product](ctx, db, "01HQ3K8V2X...")

// ... time passes, another process may have updated the document ...

err := den.Refresh(ctx, db, product)
// product now reflects the latest state in the database
```

If the document has been deleted, `Refresh` returns `ErrNotFound`.
