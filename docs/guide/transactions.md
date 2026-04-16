# Transactions

Den provides explicit transactions for operations that must be atomic across multiple documents or collections.

## RunInTransaction

All transactional work is wrapped in `den.RunInTransaction`. Return `nil` to commit, return an error to roll back.

```go
err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    // All operations here share the same database transaction.
    // Reads see a consistent snapshot.

    sender, err := den.TxFindByID[Account](tx, senderID)
    if err != nil {
        return err // rolls back
    }

    receiver, err := den.TxFindByID[Account](tx, receiverID)
    if err != nil {
        return err // rolls back
    }

    sender.Balance -= amount
    receiver.Balance += amount

    if err := den.TxUpdate(tx, sender); err != nil {
        return err // rolls back
    }
    if err := den.TxUpdate(tx, receiver); err != nil {
        return err // rolls back
    }

    return nil // commits
})
```

## Transaction Functions

Inside a transaction closure, use the `Tx`-prefixed variants of Den's CRUD functions:

| Standard API | Transaction API |
|---|---|
| `den.FindByID[T](ctx, db, id)` | `den.TxFindByID[T](tx, id)` |
| `den.Insert(ctx, db, &doc)` | `den.TxInsert(tx, &doc)` |
| `den.Update(ctx, db, &doc)` | `den.TxUpdate(tx, &doc)` |
| `den.Delete(ctx, db, &doc)` | `den.TxDelete(tx, &doc)` |
| — (transaction-only) | `den.TxLockByID[T](tx, id)` |

These functions operate on the `*den.Tx` instead of the `*den.DB`, ensuring all reads and writes go through the same underlying database transaction.

## Row-Level Locking

`den.TxLockByID[T](tx, id)` reads a document and acquires a row-level lock that persists for the lifetime of the transaction. Other transactions that try to lock the same row block until this transaction commits or rolls back.

```go
err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    item, err := den.TxLockByID[Inventory](tx, itemID)
    if err != nil {
        return err
    }
    if item.Stock < qty {
        return ErrOutOfStock
    }
    item.Stock -= qty
    return den.TxUpdate(tx, item)
})
```

There is deliberately no non-transaction variant: a lock outside a transaction releases immediately and would be meaningless. The `*den.Tx` parameter enforces correct usage at compile time.

=== "PostgreSQL"

    Emits `SELECT ... FOR UPDATE`. The lock is held until the enclosing transaction commits or rolls back. Concurrent transactions attempting to lock the same row block until the holder releases.

=== "SQLite"

    No-op. IMMEDIATE transactions already serialize writers at the database level, so per-row locking adds nothing. `TxLockByID` behaves identically to `TxFindByID` on SQLite.

!!! tip
    For most read-modify-write scenarios, **revision control** (`den.Settings{UseRevision: true}`) is the better choice — it works identically across both backends and does not hold database locks. Reach for `TxLockByID` when contention is high enough that retry storms are a concern, when the business logic between read and write is too expensive to repeat on conflict, or when you need a queue-consumer pattern.

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
    job, err := den.TxFindByID[Job](tx, jobID)
    if err != nil {
        return err
    }

    if job.Status != "pending" {
        return fmt.Errorf("job already claimed")
    }

    job.Status = "running"
    job.StartedAt = time.Now()

    return den.TxUpdate(tx, job)
})
```

!!! note
    For single-document atomic updates, consider `den.FindOneAndUpdate` which handles the find-modify-save pattern in one call without requiring an explicit transaction.
