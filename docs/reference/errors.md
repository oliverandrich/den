# Error Types

All Den errors are typed sentinel values that support `errors.Is()` and `errors.As()` for reliable error handling.

Import: `github.com/oliverandrich/den`

---

## Error Reference

| Error | Description | When Returned |
|---|---|---|
| `ErrNotFound` | Document lookup yielded no result | `FindByID`, `First`, `FindOneAndUpdate`, `Refresh` when no matching document exists |
| `ErrMultipleMatches` | A single-document lookup matched more than one row | `FindOneAndUpdate`, `FindOneAndUpsert` when conditions match more than one document. The conditions must identify the document uniquely |
| `ErrDuplicate` | Unique index constraint violated | `Insert`, `Update` when a document with the same unique field value already exists |
| `ErrRevisionConflict` | Optimistic concurrency check failed | `Update` when the document's `_rev` does not match the stored revision (another process modified it) |
| `ErrNotRegistered` | Operating on an unregistered document type | Any CRUD or query operation on a type not passed to `den.Register()` |
| `ErrValidation` | Validation hook returned an error | `Insert`, `Update` when the document's `Validate()` method or struct tag validation fails |
| `ErrTransactionFailed` | Transaction could not be committed | `RunInTransaction` when the commit fails |
| `ErrNoSnapshot` | No stored snapshot to revert to | `Revert` when the document was never loaded from the database or does not embed `Tracked` |
| `ErrMigrationFailed` | A migration function returned an error | `Registry.Up`, `Registry.UpOne` when a migration fails; wraps the original error with the migration version |
| `ErrLocked` | Row is locked by another transaction | `LockByID` with `NoWait()` when another transaction holds the row lock (PostgreSQL only; SQLite never returns this) |
| `ErrDeadlock` | PostgreSQL reported a deadlock between transactions | Any operation on PostgreSQL when the server cancels the query with SQLSTATE `40P01`. Callers can `errors.Is(err, den.ErrDeadlock)` and retry the transaction. SQLite never returns this |
| `ErrSerialization` | Serializable or repeatable-read transaction could not be serialized | PostgreSQL SQLSTATE `40001`. Becomes relevant once callers opt into stricter isolation levels; standard Den operations using the default isolation level rarely see this |
| `ErrFTSNotSupported` | Backend does not implement full-text search | `QuerySet.Search` when the active backend does not provide an `FTSProvider` implementation |
| `ErrLockRequiresTransaction` | `QuerySet.ForUpdate` used on a `*DB` scope | Terminal methods (`All`, `First`, `Count`, …) on a QuerySet where `ForUpdate` was set but the scope is not a `*Tx`. Row locking is meaningless outside a transaction |
| `ErrIncompatibleScope` | A CRUDOption requires a different scope than the caller passed | `InsertMany` with `ContinueOnError()` called against a `*Tx` (the caller's transaction cannot be split into per-document transactions) |
| `ErrIncompatibleOptions` | Two mutually-exclusive CRUDOptions were combined | `InsertMany` with both `PreValidate()` and `ContinueOnError()` |
| `*InsertManyError` | Per-document failures from `InsertMany` with `ContinueOnError()` | Carries `[]InsertFailure{Index, Err}`. Implements `Unwrap() []error`, so `errors.Is` matches any wrapped sentinel. Returned only when at least one document failed; nil otherwise |

---

## Usage with errors.Is

```go
product, err := den.FindByID[Product](ctx, db, "nonexistent-id")
if errors.Is(err, den.ErrNotFound) {
    // handle missing document
    log.Println("product not found")
}
```

```go
err := den.Insert(ctx, db, &product)
if errors.Is(err, den.ErrDuplicate) {
    // handle unique constraint violation
    log.Println("a product with that SKU already exists")
}
```

```go
err := den.Update(ctx, db, &product)
if errors.Is(err, den.ErrRevisionConflict) {
    // re-read and retry, or inform the user
    log.Println("document was modified by another process")
}
```

---

## Validation Errors

When using the `validate` sub-package (`github.com/oliverandrich/den/validate`), validation errors can be unwrapped to access per-field details:

```go
err := den.Insert(ctx, db, &user)
if errors.Is(err, den.ErrValidation) {
    var ve *validate.Errors
    if errors.As(err, &ve) {
        for _, fieldErr := range ve.Fields {
            log.Printf("field %s: %s", fieldErr.Field, fieldErr.Message)
        }
    }
}
```

---

## Error Wrapping

All Den errors can be wrapped with additional context. The original sentinel remains matchable via `errors.Is()`:

```go
// Inside Den, errors are wrapped like this:
return fmt.Errorf("insert into %s: %w", collection, den.ErrDuplicate)

// Your code can still match:
errors.Is(err, den.ErrDuplicate) // true
```
