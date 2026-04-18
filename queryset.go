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
// the query is only executed when a terminal method (All, First, Count, etc.) is called.
//
// The zero value is not usable — always obtain a QuerySet via NewQuery.
// Calling terminal methods on a zero-value QuerySet panics because the
// backend reference is nil.
type QuerySet[T any] struct {
	db             *DB
	conditions     []where.Condition
	sortFields     []SortEntry
	limitN         int
	skipN          int
	afterID        string
	beforeID       string
	fetchLinks     bool
	nestDepth      int
	includeDeleted bool
}

// NewQuery creates a new QuerySet. Conditions can optionally be passed directly.
// The context is supplied later when a terminal method (All, First, Iter, …) runs,
// so the same QuerySet can be executed against different contexts.
func NewQuery[T any](db *DB, conditions ...where.Condition) QuerySet[T] {
	qs := QuerySet[T]{db: db, nestDepth: 3}
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

// --- Terminal methods (execute the query) ---

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
	col, err := collectionFor[T](qs.db)
	if err != nil {
		return nil, err
	}

	q := qs.buildBackendQuery(col)

	iter, err := qs.db.backend.Query(ctx, col.meta.Name, q)
	if err != nil {
		return nil, err
	}
	results, err := drainIter[T](qs.db, iter, qs.limitN)
	_ = iter.Close()
	if err != nil {
		return nil, err
	}

	if err := batchResolveLinks(ctx, qs.db, qs.db.backend, results, qs.nestDepth); err != nil {
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
	col, err := collectionFor[T](qs.db)
	if err != nil {
		return 0, err
	}

	q := qs.buildBackendQuery(col)
	return qs.db.backend.Count(ctx, col.meta.Name, q)
}

// Exists returns true if at least one document matches.
func (qs QuerySet[T]) Exists(ctx context.Context) (bool, error) {
	col, err := collectionFor[T](qs.db)
	if err != nil {
		return false, err
	}

	q := qs.buildBackendQuery(col)
	return qs.db.backend.Exists(ctx, col.meta.Name, q)
}

// AllWithCount returns matching documents and the total unpaginated count.
func (qs QuerySet[T]) AllWithCount(ctx context.Context) ([]*T, int64, error) {
	col, err := collectionFor[T](qs.db)
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

	// Run Count + Query in a single read transaction for consistency
	tx, err := qs.db.backend.Begin(ctx, false)
	if err != nil {
		return nil, 0, fmt.Errorf("begin read tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	total, err := tx.Count(ctx, col.meta.Name, countQ)
	if err != nil {
		return nil, 0, err
	}

	// Drain the iterator fully BEFORE running link lookups. pgx pins the
	// connection to the open rows; issuing tx.Get while the iterator is
	// live returns "conn busy". Materialize results first, close, then
	// resolve links on the same tx (and therefore the same pooled conn).
	iter, err := tx.Query(ctx, col.meta.Name, dataQ)
	if err != nil {
		return nil, 0, err
	}
	results, err := drainIter[T](qs.db, iter, qs.limitN)
	_ = iter.Close()
	if err != nil {
		return nil, 0, err
	}

	if qs.fetchLinks {
		if err := batchResolveLinks(ctx, qs.db, tx, results, qs.nestDepth); err != nil {
			return nil, 0, err
		}
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
func drainIter[T any](db *DB, iter Iterator, capHint int) ([]*T, error) {
	var results []*T
	if capHint > 0 {
		results = make([]*T, 0, capHint)
	}
	for iter.Next() {
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

// Update applies field updates to all matching documents in a single transaction.
// Returns the number of updated documents.
func (qs QuerySet[T]) Update(ctx context.Context, fields SetFields) (int64, error) {
	col, err := collectionFor[T](qs.db)
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
	txErr := RunInTransaction(ctx, qs.db, func(tx *Tx) error {
		// Drain the iterator to completion before issuing any writes. pgx's
		// cursor pins the transaction's connection, so running TxUpdate while
		// the cursor is still open would hit "conn busy" on PostgreSQL.
		iter, err := tx.tx.Query(ctx, col.meta.Name, q)
		if err != nil {
			return err
		}
		var docs []*T
		for iter.Next() {
			doc := new(T)
			if err := decodeIterRow(qs.db, iter.Bytes(), doc); err != nil {
				_ = iter.Close()
				return fmt.Errorf("decode: %w", err)
			}
			docs = append(docs, doc)
		}
		iterErr := iter.Err()
		_ = iter.Close()
		if iterErr != nil {
			return iterErr
		}

		for _, doc := range docs {
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
	})
	if txErr != nil {
		return 0, txErr
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
	}

	q.Conditions = append(q.Conditions, qs.allConditions(col)...)

	return q
}

// allConditions returns all conditions including auto-injected soft-delete filter.
func (qs QuerySet[T]) allConditions(col *collectionInfo) []where.Condition {
	conditions := qs.conditions
	if col.meta.HasSoftBase && !qs.includeDeleted {
		conditions = append(slices.Clone(conditions), where.Field("_deleted_at").IsNil())
	}
	return conditions
}
