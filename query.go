package den

import (
	"context"

	"github.com/oliverandrich/den/internal/core"
	"github.com/oliverandrich/den/where"
)

// NewQuery creates a new chainable query for type T. Scope is `*DB`
// (outside a transaction) or `*Tx` (inside one). The context is supplied
// later by the terminal method, so one `QuerySet` can be reused across
// contexts.
func NewQuery[T any](scope Scope, conditions ...where.Condition) QuerySet[T] {
	return core.NewQuery[T](scope, conditions...)
}

// RunInTransaction opens a write transaction on db, runs fn inside it,
// and commits on success. Any non-nil error from fn rolls back the
// transaction; the same error is returned to the caller.
func RunInTransaction(ctx context.Context, db *DB, fn func(tx *Tx) error) error {
	return core.RunInTransaction(ctx, db, fn)
}

// LockByID loads a document with a row-level lock held until the
// surrounding transaction commits or rolls back. Must be called inside a
// transaction; ErrLockRequiresTransaction otherwise.
func LockByID[T any](ctx context.Context, tx *Tx, id string, opts ...LockOption) (*T, error) {
	return core.LockByID[T](ctx, tx, id, opts...)
}

// AdvisoryLock acquires a transaction-scoped advisory lock identified by
// key. PostgreSQL only; SQLite no-ops.
func AdvisoryLock(ctx context.Context, tx *Tx, key int64) error {
	return core.AdvisoryLock(ctx, tx, key)
}
