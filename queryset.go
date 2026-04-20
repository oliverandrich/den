package den

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/oliverandrich/den/internal"
	"github.com/oliverandrich/den/where"
)

// QuerySet is a lazy, immutable query builder. Chain methods return copies;
// the query is only executed when a terminal method (All, First, Count, etc.)
// is called.
//
// QuerySet binds to a Scope — either a *DB (operating outside a transaction)
// or a *Tx (operating inside RunInTransaction). Row-level locking via
// ForUpdate is only valid on a *Tx scope; calling it on a *DB-bound QuerySet
// defers an error that surfaces on the terminal method.
//
// The zero value is not usable — always obtain a QuerySet via NewQuery.
// Calling terminal methods on a zero-value QuerySet panics because the
// scope reference is nil.
type QuerySet[T any] struct {
	scope          Scope
	conditions     []where.Condition
	sortFields     []SortEntry
	limitN         int
	skipN          int
	afterID        string
	beforeID       string
	fetchLinks     bool
	nestDepth      int
	includeDeleted bool
	// lock is set by ForUpdate. Only meaningful when scope is *Tx; terminal
	// methods surface ErrLockRequiresTransaction otherwise.
	lock *LockMode
	// err captures a deferred error from a chainable method (notably
	// ForUpdate with contradictory options, or any use of ForUpdate on a
	// *DB scope at terminal time) so it can surface on the terminal call.
	err error
}

// NewQuery creates a new QuerySet bound to the given scope. Conditions can
// optionally be passed directly. The context is supplied later when a
// terminal method (All, First, Iter, …) runs, so the same QuerySet can be
// executed against different contexts.
//
// Pass a *DB for queries outside a transaction, or a *Tx from within a
// RunInTransaction closure for a query that sees the transaction's view of
// the data. Use ForUpdate only on a *Tx-bound QuerySet.
func NewQuery[T any](scope Scope, conditions ...where.Condition) QuerySet[T] {
	qs := QuerySet[T]{scope: scope, nestDepth: 3}
	if len(conditions) > 0 {
		qs.conditions = conditions
	}
	return qs
}

// --- Chain methods (return copies) ---

// Where adds filter conditions. Multiple calls are ANDed.
func (qs QuerySet[T]) Where(conditions ...where.Condition) QuerySet[T] {
	qs.conditions = append(slices.Clone(qs.conditions), conditions...)
	return qs
}

// Sort adds a sort criterion. Multiple calls define tie-breakers.
func (qs QuerySet[T]) Sort(field string, dir SortDirection) QuerySet[T] {
	qs.sortFields = append(slices.Clone(qs.sortFields), SortEntry{Field: field, Dir: dir})
	return qs
}

// Limit sets the maximum number of results.
func (qs QuerySet[T]) Limit(n int) QuerySet[T] {
	qs.limitN = n
	return qs
}

// Skip sets the number of results to skip.
func (qs QuerySet[T]) Skip(n int) QuerySet[T] {
	qs.skipN = n
	return qs
}

// After sets the cursor for forward pagination.
func (qs QuerySet[T]) After(id string) QuerySet[T] {
	qs.afterID = id
	return qs
}

// Before sets the cursor for backward pagination.
func (qs QuerySet[T]) Before(id string) QuerySet[T] {
	qs.beforeID = id
	return qs
}

// WithFetchLinks enables eager loading of linked documents.
func (qs QuerySet[T]) WithFetchLinks() QuerySet[T] {
	qs.fetchLinks = true
	return qs
}

// WithNestingDepth sets the maximum link resolution depth.
func (qs QuerySet[T]) WithNestingDepth(depth int) QuerySet[T] {
	qs.nestDepth = depth
	return qs
}

// IncludeDeleted includes soft-deleted documents in the results.
func (qs QuerySet[T]) IncludeDeleted() QuerySet[T] {
	qs.includeDeleted = true
	return qs
}

// ForUpdate acquires a row-level lock on every matching document, held until
// the enclosing transaction commits or rolls back. Only valid on a QuerySet
// bound to a *Tx — on a *DB-bound QuerySet the call is accepted but the
// terminal method will return ErrLockRequiresTransaction.
//
// Pass SkipLocked to omit locked rows from the result set (queue-consumer
// pattern) or NoWait to fail immediately with ErrLocked when a row is held
// by another transaction. On SQLite these options are no-ops because
// IMMEDIATE transactions already serialize writers.
//
// Passing both SkipLocked and NoWait is a programmer error (PG allows only
// one); ForUpdate captures the error on the query set and surfaces it when
// a terminal method runs.
func (qs QuerySet[T]) ForUpdate(opts ...LockOption) QuerySet[T] {
	cfg := lockConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	mode, err := cfg.resolve()
	if err != nil {
		qs.err = err
		return qs
	}
	qs.lock = &mode
	return qs
}

// --- Terminal methods (execute the query) ---

// preflight checks deferred errors set by chain methods and enforces the
// ForUpdate-requires-*Tx constraint. Every terminal method that touches the
// lock state calls this first so the error surfaces consistently.
func (qs QuerySet[T]) preflight() error {
	if qs.err != nil {
		return qs.err
	}
	if qs.lock != nil {
		if _, ok := qs.scope.(*Tx); !ok {
			return ErrLockRequiresTransaction
		}
	}
	return nil
}

// All executes the query and returns all matching documents.
//
// With WithFetchLinks enabled, All drains the result set first and then
// resolves every link field in batched IN-queries (one per target type per
// nesting level) instead of the per-row Get that streaming .Iter() does.
// For N parents sharing a small set of linked targets this collapses
// N round-trips into one — at the cost of buffering the full result set,
// which is already implicit in .All()'s contract. Callers who need true
// streaming with link resolution should keep using .Iter().
func (qs QuerySet[T]) All(ctx context.Context) ([]*T, error) {
	if err := qs.preflight(); err != nil {
		return nil, err
	}
	if qs.fetchLinks {
		return qs.allBatched(ctx)
	}
	// Pre-allocate when limit is known to avoid repeated slice growth.
	var results []*T
	if qs.limitN > 0 {
		results = make([]*T, 0, qs.limitN)
	}
	for doc, err := range qs.Iter(ctx) {
		if err != nil {
			return nil, err
		}
		results = append(results, doc)
	}
	return results, nil
}

// allBatched is the .All() implementation used when fetchLinks is enabled.
// It drains the iterator fully before running the batched link resolver
// so pgx's cursor has released the connection by the time the resolver
// issues its IN-query through the same ReadWriter.
func (qs QuerySet[T]) allBatched(ctx context.Context) ([]*T, error) {
	db := qs.scope.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, err
	}

	q := qs.buildBackendQuery(col)
	rw := qs.scope.readWriter()

	iter, err := rw.Query(ctx, col.meta.Name, q)
	if err != nil {
		return nil, err
	}
	results, err := drainIter[T](ctx, db, iter, qs.limitN)
	_ = iter.Close()
	if err != nil {
		return nil, err
	}

	if err := batchResolveLinks(ctx, db, rw, results, qs.nestDepth); err != nil {
		return nil, err
	}
	return results, nil
}

// First returns the first matching document. Returns ErrNotFound if none match.
func (qs QuerySet[T]) First(ctx context.Context) (*T, error) {
	results, err := qs.Limit(1).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, ErrNotFound
	}
	return results[0], nil
}

// Count returns the number of matching documents.
func (qs QuerySet[T]) Count(ctx context.Context) (int64, error) {
	if err := qs.preflight(); err != nil {
		return 0, err
	}
	col, err := collectionFor[T](qs.scope.db())
	if err != nil {
		return 0, err
	}

	q := qs.buildBackendQuery(col)
	return qs.scope.readWriter().Count(ctx, col.meta.Name, q)
}

// Exists returns true if at least one document matches.
func (qs QuerySet[T]) Exists(ctx context.Context) (bool, error) {
	if err := qs.preflight(); err != nil {
		return false, err
	}
	col, err := collectionFor[T](qs.scope.db())
	if err != nil {
		return false, err
	}

	q := qs.buildBackendQuery(col)
	return qs.scope.readWriter().Exists(ctx, col.meta.Name, q)
}

// AllWithCount returns matching documents and the total unpaginated count.
//
// When the QuerySet is bound to a *DB, the count+query run in a read
// transaction for consistency. When bound to a *Tx, they run through the
// existing transaction and no nested tx is opened.
func (qs QuerySet[T]) AllWithCount(ctx context.Context) ([]*T, int64, error) {
	if err := qs.preflight(); err != nil {
		return nil, 0, err
	}
	db := qs.scope.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, 0, err
	}

	// Build both queries upfront
	countQS := qs
	countQS.limitN = 0
	countQS.skipN = 0
	countQS.sortFields = nil
	countQS.afterID = ""
	countQS.beforeID = ""
	countQ := countQS.buildBackendQuery(col)
	dataQ := qs.buildBackendQuery(col)

	run := func(rw ReadWriter) ([]*T, int64, error) {
		total, err := rw.Count(ctx, col.meta.Name, countQ)
		if err != nil {
			return nil, 0, err
		}

		// Drain the iterator fully BEFORE running link lookups. pgx pins the
		// connection to the open rows; issuing rw.Get while the iterator is
		// live returns "conn busy".
		iter, err := rw.Query(ctx, col.meta.Name, dataQ)
		if err != nil {
			return nil, 0, err
		}
		results, err := drainIter[T](ctx, db, iter, qs.limitN)
		_ = iter.Close()
		if err != nil {
			return nil, 0, err
		}

		if qs.fetchLinks {
			if err := batchResolveLinks(ctx, db, rw, results, qs.nestDepth); err != nil {
				return nil, 0, err
			}
		}
		return results, total, nil
	}

	// When already inside a transaction, reuse it.
	if tx, ok := qs.scope.(*Tx); ok {
		return run(tx.tx)
	}

	// DB path: open a read tx so count+query see a consistent snapshot.
	tx, err := db.backend.Begin(ctx, false)
	if err != nil {
		return nil, 0, fmt.Errorf("begin read tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	results, total, err := run(tx)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrTransactionFailed, err)
	}
	return results, total, nil
}

// drainIter materializes all remaining rows from iter into a new []*T. The
// iterator is not closed by this helper — callers own its lifetime so a
// post-drain batch link resolver can safely route through the same read
// transaction that owns the iterator without hitting pgx's "conn busy".
//
// Cancelling ctx terminates the drain within at most one row; the check
// runs at the top of each iteration, matching the cancellation contract
// QuerySet.Iter documents.
func drainIter[T any](ctx context.Context, db *DB, iter Iterator, capHint int) ([]*T, error) {
	var results []*T
	if capHint > 0 {
		results = make([]*T, 0, capHint)
	}
	for iter.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		doc := new(T)
		if err := decodeIterRow(db, iter.Bytes(), doc); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		results = append(results, doc)
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// Update applies field updates to every matching document. Returns the
// number of updated documents.
//
// When bound to a *DB, the scan + writes run in a new transaction so the
// batch is atomic. When bound to a *Tx, they run inline in the caller's
// transaction — a per-row failure rolls back the caller's transaction too.
//
// Update is fail-fast: any per-row error (BeforeUpdate hook, validation,
// revision conflict, backend write) stops the loop, rolls back the
// transaction, and returns (0, err). There is no partial commit; no
// AfterUpdate / AfterSave hooks fire for rows that would have come after
// the failure.
//
// Field names in fields (as they appear in the `json` struct tag) are
// validated against the registered struct before the write transaction
// opens — an unknown name returns immediately without opening the tx.
// Callers that want to validate field names at application start can
// iterate Meta[T].Fields.
func (qs QuerySet[T]) Update(ctx context.Context, fields SetFields) (int64, error) {
	if err := qs.preflight(); err != nil {
		return 0, err
	}
	db := qs.scope.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return 0, err
	}

	// Validate and cache field lookups before starting the transaction
	fieldInfos := make(map[string]*internal.FieldInfo, len(fields))
	for fieldName := range fields {
		fi := col.structInfo.FieldByName(fieldName)
		if fi == nil {
			return 0, fmt.Errorf("den: field %q not found in %s", fieldName, col.meta.Name)
		}
		fieldInfos[fieldName] = fi
	}

	q := qs.buildBackendQuery(col)

	var count int64
	body := func(tx *Tx) error {
		// Drain the iterator to completion before issuing any writes. pgx's
		// cursor pins the transaction's connection, so running Update while
		// the cursor is still open would hit "conn busy" on PostgreSQL.
		iter, err := tx.tx.Query(ctx, col.meta.Name, q)
		if err != nil {
			return err
		}
		docs, err := drainIter[T](ctx, db, iter, qs.limitN)
		_ = iter.Close()
		if err != nil {
			return err
		}

		for _, doc := range docs {
			if err := ctx.Err(); err != nil {
				return err
			}
			rv := reflect.ValueOf(doc).Elem()
			for fieldName, newVal := range fields {
				fv := rv.FieldByIndex(fieldInfos[fieldName].Index)
				if err := setFieldValue(fv, newVal, fieldName); err != nil {
					return err
				}
			}
			if err := Update(ctx, tx, doc); err != nil {
				return err
			}
			count++
		}
		return nil
	}

	if err := runOnScopeVoid(ctx, qs.scope, body); err != nil {
		return 0, err
	}
	return count, nil
}

// --- Internal helpers ---

// buildBackendQuery converts the QuerySet into a backend Query struct.
func (qs QuerySet[T]) buildBackendQuery(col *collectionInfo) *Query {
	q := &Query{
		Collection: col.meta.Name,
		SortFields: qs.sortFields,
		LimitN:     qs.limitN,
		SkipN:      qs.skipN,
		AfterID:    qs.afterID,
		BeforeID:   qs.beforeID,
		Lock:       qs.lock,
	}

	q.Conditions = append(q.Conditions, qs.allConditions(col)...)

	return q
}

// allConditions returns all conditions including auto-injected soft-delete filter.
func (qs QuerySet[T]) allConditions(col *collectionInfo) []where.Condition {
	conditions := qs.conditions
	if col.meta.HasSoftDelete && !qs.includeDeleted {
		conditions = append(slices.Clone(conditions), where.Field("_deleted_at").IsNil())
	}
	return conditions
}
