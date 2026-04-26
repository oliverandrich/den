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

Insert multiple documents in a single batch transaction:

```go
products := []*Product{
    {Name: "Widget", Price: 9.99},
    {Name: "Gadget", Price: 19.99},
    {Name: "Doohickey", Price: 4.99},
}
err := den.InsertMany(ctx, db, products)
```

By default the entire batch runs inside one transaction: a failure on any document rolls back every successful predecessor. Two options change that behavior:

**`PreValidate()`** runs the full insert hook + validation chain on every document *before* the write transaction opens. If any document fails validation, no writes are attempted — useful for large imports where a late-failing document would otherwise waste the work of every successful predecessor. Hooks fire exactly once per document: `BeforeInsert` / `BeforeSave` / `Validate` run in the pre-pass, and the in-transaction commit only performs the Put and `AfterInsert` / `AfterSave`. Combining with `WithLinkRule(LinkWrite)` disables the caching optimization (cascade has to run inside the tx), so hooks fire twice on that specific combination.

```go
err := den.InsertMany(ctx, db, products, den.PreValidate())
if errors.Is(err, den.ErrValidation) {
    // a document failed pre-validation; nothing was written
}
```

**`ContinueOnError()`** writes each document in its own short-lived transaction and returns an `*InsertManyError` listing per-document failures by input index. Successful inserts are committed, so partial success is visible. Cannot be combined with a `*Tx` scope.

```go
err := den.InsertMany(ctx, db, products, den.ContinueOnError())
var multi *den.InsertManyError
if errors.As(err, &multi) {
    for _, f := range multi.Failures {
        log.Printf("doc[%d]: %v", f.Index, f.Err)
    }
}
```

The two options cannot be combined — `InsertMany` returns `ErrIncompatibleOptions`. Each doc-level transaction in `ContinueOnError` would make a global pre-pass guarantee meaningless, so the rejection is intentional.

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
).Update(ctx, den.SetFields{"category": "new"})  // keys are JSON tag names ("category"), not Go field names ("Category")
// count = number of documents updated
```

!!! warning "`SetFields` keys are JSON tag names"
    Every `SetFields{...}` map uses the JSON tag name (`"category"`,
    `"price"`, `"login_count"`), NOT the Go field name. Mixing them up
    fails fast — see "Fail-fast and field validation" below — but it's
    easy to get wrong on the first try because every other Go API in
    the package uses Go field names. The same rule applies in
    `FindOneAndUpdate`, `FindOneAndUpsert`, and any other CRUD
    operation taking `SetFields`.

!!! tip
    Bulk updates are more convenient than loading, modifying, and saving each document individually. The update runs in a single transaction, modifying each matching document individually.

### Fail-fast and field validation

`QuerySet.Update` is **fail-fast**. Any per-row error — a `BeforeUpdate` hook returning an error, validation failure, revision conflict, or backend write error — aborts the loop, rolls the transaction back, and returns `(0, err)`. There is no partial commit: either every matching document is updated, or none is.

When the query set is bound to an outer transaction (`*Tx`), a failure also rolls back that caller transaction — the error surfaces to the `RunInTransaction` closure.

Field names in `SetFields` (the names as they appear in the `json` struct tag) are validated against the registered struct **before** the write transaction opens. An unknown field returns immediately without touching storage. Callers that want to surface field-name mistakes at application start, rather than at the first `.Update()` call, can iterate `Meta[T].Fields`:

```go
meta, err := den.Meta[Product](db)
if err != nil {
    return err
}
known := make(map[string]struct{}, len(meta.Fields))
for _, f := range meta.Fields {
    known[f.Name] = struct{}{} // f.Name is the JSON name — matches SetFields keys
}
for name := range myFields {
    if _, ok := known[name]; !ok {
        return fmt.Errorf("unknown field %q on Product", name)
    }
}
```

Den does not ship a typed `SetFields` builder: a chained generic alternative would not give meaningfully more safety than the runtime check, and compile-time field access would require code generation, which is outside the current scope.

## FindOneAndUpdate

Atomic find-and-modify in a single transaction. Finds the document matching the conditions, applies the field updates, and returns the modified document.

The conditions must identify the document uniquely. If they match more than one row, `FindOneAndUpdate` returns `ErrMultipleMatches` instead of silently picking one. If they match nothing, it returns `ErrNotFound`.

```go
counter, err := den.FindOneAndUpdate[Counter](ctx, db,
    den.SetFields{"value": newValue},
    []where.Condition{where.Field("name").Eq("downloads")}, // "name" is unique
)
if errors.Is(err, den.ErrNotFound) {
    // no row named "downloads" exists yet
}
if errors.Is(err, den.ErrMultipleMatches) {
    // schema bug: name should be unique but isn't
}
```

Pass `den.IncludeDeleted()` to also match soft-deleted documents.

!!! tip "Job-queue pattern"
    For claim-one-of-many patterns (job queues, work tickets), reach for `RunInTransaction` together with `QuerySet.ForUpdate(SkipLocked())` — that locks the row at SELECT time so a concurrent worker skips it instead of racing for the same write.

## FindOneAndUpsert

Find an existing document or insert a new one if none matches, then apply field updates. The `defaults` template is used only on the insert path; `fields` is applied on both paths.

```go
user, inserted, err := den.FindOneAndUpsert[User](ctx, db,
    &User{Email: "x@y.z", LoginCount: 0}, // insert template
    den.SetFields{"login_count": 5},      // applied on both paths
    []where.Condition{where.Field("email").Eq("x@y.z")},
)
if err != nil {
    // ...
}
if inserted {
    log.Println("created new user")
}
```

Like `FindOneAndUpdate`, conditions must match at most one document — `ErrMultipleMatches` otherwise. Soft-deleted matches are skipped by default; pass `IncludeDeleted()` to update them in place.

!!! note "Concurrency"
    Two concurrent upserts that both miss race for the insert. One wins; the other gets `ErrDuplicate` from the underlying unique constraint on the lookup column. There is no internal retry — callers that want one decide explicitly between retry and surfacing the error.

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
