# Transactions

Den provides explicit transactions for operations that must be atomic across multiple documents or collections.

## RunInTransaction

All transactional work is wrapped in `den.RunInTransaction`. Return `nil` to commit, return an error to roll back.

```go
err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    // All operations here share the same database transaction.
    // Reads see a consistent snapshot.

    sender, err := den.FindByID[Account](ctx, tx, senderID)
    if err != nil {
        return err // rolls back
    }

    receiver, err := den.FindByID[Account](ctx, tx, receiverID)
    if err != nil {
        return err // rolls back
    }

    sender.Balance -= amount
    receiver.Balance += amount

    if err := den.Update(ctx, tx, sender); err != nil {
        return err // rolls back
    }
    if err := den.Update(ctx, tx, receiver); err != nil {
        return err // rolls back
    }

    return nil // commits
})
```

## Transaction Functions

CRUD functions accept a `den.Scope` interface satisfied by both `*den.DB` and `*den.Tx` — pass whichever you have. The same `den.Insert(ctx, scope, &doc)` works outside and inside a transaction:

```go
// Outside a transaction
den.Insert(ctx, db, &product)

// Inside a transaction — same function, same signature
den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    return den.Insert(ctx, tx, &product)
})
```

`Scope` is sealed: only `*DB` and `*Tx` satisfy it. Backend authors do not need to care — they implement `Backend` / `Transaction` as before.

A handful of APIs remain transaction-only because their semantics are tied to transaction lifetime (a lock outside a transaction would release immediately; raw bytes without rollback would leak half-written state). They take `*den.Tx` directly rather than `Scope`:

| API | Why transaction-only |
|---|---|
| `den.LockByID[T](ctx, tx, id, opts...)` | Row-level lock released on commit/rollback |
| `den.AdvisoryLock(ctx, tx, key)` | Application-level lock released on commit/rollback |
| `den.NewQuery[T](tx).ForUpdate(...)` | Multi-row `FOR UPDATE` locking (QuerySet refuses to run `ForUpdate` against a `*DB` scope) |
| `tx.Transaction()` | Low-level escape hatch to the underlying backend Transaction. Only for infrastructure code like the migration log — application code should use `Insert` / `Update` / `Delete` / `FindByID` etc. |

## Row-Level Locking

`den.LockByID[T](ctx, tx, id)` reads a document and acquires a row-level lock that persists for the lifetime of the transaction. Other transactions that try to lock the same row block until this transaction commits or rolls back.

```go
err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    item, err := den.LockByID[Inventory](ctx, tx, itemID)
    if err != nil {
        return err
    }
    if item.Stock < qty {
        return ErrOutOfStock
    }
    item.Stock -= qty
    return den.Update(ctx, tx, item)
})
```

There is deliberately no non-transaction variant: a lock outside a transaction releases immediately and would be meaningless. The `*den.Tx` parameter enforces correct usage at compile time.

=== "PostgreSQL"

    Emits `SELECT ... FOR UPDATE`. The lock is held until the enclosing transaction commits or rolls back. Concurrent transactions attempting to lock the same row block until the holder releases.

=== "SQLite"

    No-op. IMMEDIATE transactions already serialize writers at the database level, so per-row locking adds nothing. `LockByID` behaves identically to `FindByID` on SQLite.

!!! tip
    For most read-modify-write scenarios, **revision control** (`den.Settings{UseRevision: true}`) is the better choice — it works identically across both backends and does not hold database locks. Reach for `LockByID` when contention is high enough that retry storms are a concern, when the business logic between read and write is too expensive to repeat on conflict, or when you need a queue-consumer pattern.

### Lock Modifiers

Two options change how `LockByID` reacts to contention on PostgreSQL:

- `den.SkipLocked()` — if another transaction holds the row, the query returns no rows. Mapped to `FOR UPDATE SKIP LOCKED`. The canonical queue-consumer primitive: N workers can each pop a different row without blocking each other.
- `den.NoWait()` — if another transaction holds the row, fail immediately with `den.ErrLocked`. Mapped to `FOR UPDATE NOWAIT`. Use when the caller should choose between retry, abort, or an alternative path rather than wait.

`SkipLocked()` and `NoWait()` are **mutually exclusive** — PostgreSQL allows only one. Passing both to `LockByID` returns an error; passing both to `TxQuerySet.ForUpdate` captures the error on the query set and surfaces it when you call `All` or `First`.

On SQLite both options are no-ops (writers are serialized at the database level).

```go
// Queue worker pattern: pop next unlocked job
err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    job, err := den.LockByID[Job](ctx, tx, candidateID, den.SkipLocked())
    if errors.Is(err, den.ErrNotFound) {
        return nil // another worker owns it (or it really does not exist)
    }
    if err != nil {
        return err
    }
    return processJob(ctx, tx, job)
})
```

!!! warning "SKIP LOCKED returns `ErrNotFound`"
    PostgreSQL returns zero rows for both "locked by another tx" and "row does not exist" when `SKIP LOCKED` is active — these cases are indistinguishable through the error alone. If you need to tell them apart, do a separate non-locking read first.

### Multi-row Locking

`NewQuery[T](tx)` bound to a `*Tx` supports `ForUpdate(opts ...LockOption)` — one SQL statement that locks every matching row, avoiding the N+1 round-trips you would get from looping over `LockByID`.

```go
err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    orders, err := den.NewQuery[Order](tx).
        Where(where.Field("customer").Eq(custID)).
        Where(where.Field("status").Eq("pending")).
        ForUpdate(den.SkipLocked()).
        All(ctx)
    if err != nil {
        return err
    }
    for _, o := range orders {
        o.Status = "processing"
        if err := den.Update(ctx, tx, o); err != nil {
            return err
        }
    }
    return nil
})
```

`ForUpdate` is legal syntactically on any `QuerySet`, but a terminal method refuses to run (`ErrLockRequiresTransaction`) when the scope is a `*DB` — a lock outside a transaction releases immediately and would be meaningless. The `SkipLocked()` and `NoWait()` options work identically to `LockByID`.

!!! tip "Deterministic lock order"
    On PostgreSQL, `ForUpdate().All(ctx)` without an explicit `Sort` emits `ORDER BY id ASC` automatically. The lock-acquisition order follows the SELECT's output order, and two concurrent callers with overlapping result sets would deadlock on PG if each walked rows in a different heap order. The default guarantees every caller locks the same way. Add your own `Sort(...)` call if you want a different order — but then it is your responsibility to keep that order consistent across callers.

## Commit and Rollback

The commit/rollback behavior is controlled entirely by the return value of the closure:

```go
err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    // ... operations ...

    if somethingWentWrong {
        return fmt.Errorf("aborting: %w", err) // transaction rolls back
    }

    return nil // transaction commits
})
```

If a panic occurs inside the closure, Den recovers it and rolls back the transaction.

## Backend Behavior

=== "SQLite"

    SQLite serializes all writers. Only one goroutine can hold a write transaction at a time; others block until the writer commits or rolls back. Readers are never blocked (WAL mode).

    ```
    Writer 1: BEGIN ──── write ──── COMMIT
    Writer 2:                               BEGIN ──── write ──── COMMIT
    Reader:   ────────── read ──────────────────────── read ──────────
    ```

=== "PostgreSQL"

    PostgreSQL uses MVCC (Multi-Version Concurrency Control). Multiple writers can run concurrently. Conflicts are detected at commit time if rows overlap.

    ```
    Writer 1: BEGIN ──── write ──── COMMIT
    Writer 2:      BEGIN ──── write ──── COMMIT
    Reader:   ────────── read ──────────── read ──────
    ```

!!! tip
    Use revision control (`den.Settings{UseRevision: true}`) for application-level conflict detection that works identically across both backends. When two processes read and modify the same document concurrently, the second `Update` returns `den.ErrRevisionConflict` regardless of backend.

## Example: Job Queue

Transactions are useful for atomic claim-and-process patterns:

```go
err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    job, err := den.FindByID[Job](ctx, tx, jobID)
    if err != nil {
        return err
    }

    if job.Status != "pending" {
        return fmt.Errorf("job already claimed")
    }

    job.Status = "running"
    job.StartedAt = time.Now()

    return den.Update(ctx, tx, job)
})
```

!!! note
    For single-document atomic updates, consider `den.FindOneAndUpdate` which handles the find-modify-save pattern in one call without requiring an explicit transaction.
