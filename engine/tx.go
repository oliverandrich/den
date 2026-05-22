package engine

import (
	"context"
	"fmt"

	"github.com/oliverandrich/den/lock"
)

// LockOption is re-exported from den/lock so engine-internal call sites
// (LockByID, QuerySet.ForUpdate) keep using the bare identifier.
type LockOption = lock.Option

// SkipLocked makes a lock acquisition return ErrNotFound immediately if
// another transaction already holds the row lock, instead of blocking.
// Maps to PostgreSQL's FOR UPDATE SKIP LOCKED; on SQLite this option
// is a no-op. Passing both SkipLocked and NoWait is an error — they are
// mutually exclusive in PostgreSQL. Thin wrapper over [lock.SkipLocked].
func SkipLocked() LockOption { return lock.SkipLocked() }

// NoWait makes a lock acquisition return ErrLocked immediately if
// another transaction already holds the row lock, instead of blocking.
// Maps to PostgreSQL's FOR UPDATE NOWAIT; on SQLite this option is a
// no-op. Passing both SkipLocked and NoWait is an error — they are
// mutually exclusive in PostgreSQL. Thin wrapper over [lock.NoWait].
func NoWait() LockOption { return lock.NoWait() }

// Tx wraps a backend Transaction for use in RunInTransaction.
//
// The zero value is not usable — construct a Tx only indirectly by passing
// a closure to RunInTransaction. Calling transaction-scoped functions on a
// zero-value Tx panics.
type Tx struct {
	parent *DB
	tx     Transaction
}

// readWriter / db together satisfy the sealed Scope interface. Unexported
// to keep Scope sealed to *DB and *Tx.
func (t *Tx) readWriter() ReadWriter { return t.tx }
func (t *Tx) db() *DB                { return t.parent }

// RunInTransaction executes fn within a transaction.
// If fn returns nil, the transaction is committed.
// If fn returns an error, the transaction is rolled back.
//
// The *Tx passed to fn does not itself carry the context; entry points
// inside fn take ctx explicitly. Use the ctx closed over from the caller.
func RunInTransaction(ctx context.Context, db *DB, fn func(tx *Tx) error) error {
	btx, err := db.backend.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	tx := &Tx{parent: db, tx: btx}

	defer func() {
		if r := recover(); r != nil {
			_ = btx.Rollback()
			panic(r)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := btx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback failed after %w: %w", err, rbErr)
		}
		return err
	}

	if err := btx.Commit(); err != nil {
		return fmt.Errorf("%w: %w", ErrTransactionFailed, err)
	}
	return nil
}

// runOnScope executes body inside a write transaction. If s is already a *Tx,
// body runs inline in the caller's transaction; otherwise a new transaction
// is opened via RunInTransaction.
//
// Centralizes the Scope-dispatch pattern used by InsertMany, DeleteMany,
// FindOneAndUpdate, FindOneAndUpsert, and QuerySet.Update. Use runOnScopeVoid
// when body only needs to return an error.
func runOnScope[T any](ctx context.Context, s Scope, body func(*Tx) (T, error)) (T, error) {
	if tx, ok := s.(*Tx); ok {
		return body(tx)
	}
	var result T
	txErr := RunInTransaction(ctx, s.db(), func(tx *Tx) error {
		r, err := body(tx)
		if err != nil {
			return err
		}
		result = r
		return nil
	})
	return result, txErr
}

// runOnScopeVoid is the error-only variant of runOnScope — for callers whose
// body either has no result or captures one via an outer-scope closure.
func runOnScopeVoid(ctx context.Context, s Scope, body func(*Tx) error) error {
	if tx, ok := s.(*Tx); ok {
		return body(tx)
	}
	return RunInTransaction(ctx, s.db(), body)
}

// runInWriteTx is an internal helper that executes fn in a write transaction.
// Used by updateCore to wrap revision-checking updates atomically.
func runInWriteTx(ctx context.Context, b Backend, fn func(tx Transaction) error) error {
	tx, err := b.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback failed after %w: %w", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: %w", ErrTransactionFailed, err)
	}
	return nil
}

// LockByID retrieves a document by ID and acquires a row-level lock that
// persists for the lifetime of the transaction. Without options, concurrent
// transactions attempting to lock the same row block until this transaction
// commits or rolls back. Pass SkipLocked or NoWait to change that behavior.
//
// On PostgreSQL this maps to SELECT ... FOR UPDATE; on SQLite it is a no-op
// because IMMEDIATE transactions already serialize writers.
//
// The *Tx parameter enforces transaction scope at compile time — a lock
// outside a transaction releases immediately and would be meaningless.
// Returns ErrNotFound if the document does not exist. Returns ErrLocked
// when NoWait is set and the row is held by another transaction.
func LockByID[T any](ctx context.Context, tx *Tx, id string, opts ...LockOption) (*T, error) {
	col, err := collectionFor[T](tx.parent)
	if err != nil {
		return nil, err
	}

	mode, err := lock.Resolve(opts...)
	if err != nil {
		return nil, err
	}

	data, err := tx.tx.GetForUpdate(ctx, col.meta.Name, id, mode)
	if err != nil {
		return nil, err
	}

	result := new(T)
	if err := decodeWithSnapshot(tx.parent, data, result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return result, nil
}

// AdvisoryLock acquires an application-defined lock on key that persists
// until the transaction commits or rolls back. Concurrent transactions
// attempting to lock the same key block until the holder ends. See the
// Transaction interface for backend-specific behavior.
func AdvisoryLock(ctx context.Context, tx *Tx, key int64) error {
	return tx.tx.AdvisoryLock(ctx, key)
}

// Transaction returns the underlying backend Transaction so infrastructure
// code can issue raw Get / Put / Delete calls on unregistered collections.
// This is a low-level escape hatch — normal code should use Insert, Update,
// Delete, FindByID, NewQuery, and friends, all of which honor the registry,
// encoding, validation, and hook contracts. The only legitimate consumer
// today is den/migrate (the migration-log collection is deliberately not
// registered with Den).
//
// Mirrors DB.Backend() in spirit: both are low-level accessors you reach
// for only when the high-level API does not cover the case.
func (t *Tx) Transaction() Transaction {
	return t.tx
}
