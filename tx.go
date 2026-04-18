package den

import (
	"context"
	"fmt"
)

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
	btx, err := db.backend.Begin(ctx, true)
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

// runInWriteTx is an internal helper that executes fn in a write transaction.
// Used by updateCore to wrap revision-checking updates atomically.
func runInWriteTx(ctx context.Context, b Backend, fn func(tx Transaction) error) error {
	tx, err := b.Begin(ctx, true)
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

// LockOption configures LockByID and TxQuerySet.ForUpdate.
type LockOption func(*lockConfig)

// lockConfig tracks each option independently (rather than collapsing to a
// single mode) so resolve() can detect and reject the caller passing both
// SkipLocked and NoWait, which are mutually exclusive in PostgreSQL.
type lockConfig struct {
	skipLocked bool
	noWait     bool
}

// resolve collapses the option flags into a single LockMode, or returns an
// error when the options contradict each other.
func (c lockConfig) resolve() (LockMode, error) {
	if c.skipLocked && c.noWait {
		return LockDefault, fmt.Errorf("den: SkipLocked and NoWait are mutually exclusive")
	}
	switch {
	case c.skipLocked:
		return LockSkipLocked, nil
	case c.noWait:
		return LockNoWait, nil
	default:
		return LockDefault, nil
	}
}

// SkipLocked makes LockByID return ErrNotFound immediately if another
// transaction already holds the row lock, instead of blocking. Maps to
// PostgreSQL's FOR UPDATE SKIP LOCKED. Useful for queue-consumer patterns
// where each worker should claim a different row without contending.
// On SQLite this option is a no-op.
//
// Because PostgreSQL returns zero rows for both "locked by another tx" and
// "row does not exist", the caller cannot distinguish these cases via the
// error alone.
//
// Passing both SkipLocked and NoWait returns an error — they are mutually
// exclusive in PostgreSQL.
func SkipLocked() LockOption {
	return func(c *lockConfig) { c.skipLocked = true }
}

// NoWait makes LockByID return ErrLocked immediately if another transaction
// already holds the row lock, instead of blocking. Maps to PostgreSQL's
// FOR UPDATE NOWAIT. Useful when the caller wants to decide between retry,
// abort, or an alternative code path. On SQLite this option is a no-op.
//
// Passing both SkipLocked and NoWait returns an error — they are mutually
// exclusive in PostgreSQL.
func NoWait() LockOption {
	return func(c *lockConfig) { c.noWait = true }
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

	cfg := lockConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	mode, err := cfg.resolve()
	if err != nil {
		return nil, err
	}

	data, err := tx.tx.GetForUpdate(ctx, col.meta.Name, id, mode)
	if err != nil {
		return nil, err
	}

	result := new(T)
	if err := tx.parent.decode(data, result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	captureSnapshot(data, result)

	return result, nil
}

// RawGet performs a raw key lookup within the transaction, returning the
// stored bytes verbatim without decoding or registry validation. Intended
// for infrastructure code that stores unregistered bookkeeping collections
// (for example, the migration log). Prefer FindByID(ctx, tx, id) for
// normal reads.
func RawGet(ctx context.Context, tx *Tx, collection, id string) ([]byte, error) {
	return tx.tx.Get(ctx, collection, id)
}

// RawPut writes raw bytes into the transaction under the given collection
// and id, bypassing encoding and registry checks. Same audience as RawGet:
// infrastructure code writing its own bookkeeping collections. Prefer
// Insert / Update for normal writes.
func RawPut(ctx context.Context, tx *Tx, collection, id string, data []byte) error {
	return tx.tx.Put(ctx, collection, id, data)
}

// AdvisoryLock acquires an application-defined lock on key that persists
// until the transaction commits or rolls back. Concurrent transactions
// attempting to lock the same key block until the holder ends. See the
// Transaction interface for backend-specific behavior.
func AdvisoryLock(ctx context.Context, tx *Tx, key int64) error {
	return tx.tx.AdvisoryLock(ctx, key)
}
