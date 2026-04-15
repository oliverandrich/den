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
type QuerySet[T any] struct {
	ctx            context.Context
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
func NewQuery[T any](ctx context.Context, db *DB, conditions ...where.Condition) QuerySet[T] {
	qs := QuerySet[T]{ctx: ctx, db: db, nestDepth: 3}
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
func (qs QuerySet[T]) All() ([]*T, error) {
	// Pre-allocate when limit is known to avoid repeated slice growth.
	var results []*T
	if qs.limitN > 0 {
		results = make([]*T, 0, qs.limitN)
	}
	for doc, err := range qs.Iter() {
		if err != nil {
			return nil, err
		}
		results = append(results, doc)
	}
	return results, nil
}

// First returns the first matching document. Returns ErrNotFound if none match.
func (qs QuerySet[T]) First() (*T, error) {
	results, err := qs.Limit(1).All()
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, ErrNotFound
	}
	return results[0], nil
}

// Count returns the number of matching documents.
func (qs QuerySet[T]) Count() (int64, error) {
	col, err := collectionFor[T](qs.db)
	if err != nil {
		return 0, err
	}

	q := qs.buildBackendQuery(col)
	return qs.db.backend.Count(qs.ctx, col.meta.Name, q)
}

// Exists returns true if at least one document matches.
func (qs QuerySet[T]) Exists() (bool, error) {
	col, err := collectionFor[T](qs.db)
	if err != nil {
		return false, err
	}

	q := qs.buildBackendQuery(col)
	return qs.db.backend.Exists(qs.ctx, col.meta.Name, q)
}

// AllWithCount returns matching documents and the total unpaginated count.
func (qs QuerySet[T]) AllWithCount() ([]*T, int64, error) {
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
	tx, err := qs.db.backend.Begin(qs.ctx, false)
	if err != nil {
		return nil, 0, fmt.Errorf("begin read tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	total, err := tx.Count(qs.ctx, col.meta.Name, countQ)
	if err != nil {
		return nil, 0, err
	}

	iter, err := tx.Query(qs.ctx, col.meta.Name, dataQ)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = iter.Close() }()

	enc := qs.db.getEncoder()
	var results []*T
	for iter.Next() {
		item := new(T)
		rawBytes := make([]byte, len(iter.Bytes()))
		copy(rawBytes, iter.Bytes())
		if err := enc.Decode(rawBytes, item); err != nil {
			return nil, 0, fmt.Errorf("decode: %w", err)
		}
		captureSnapshot(rawBytes, item)

		if qs.fetchLinks {
			if err := fetchAllLinksOnDoc(qs.ctx, qs.db, item, qs.nestDepth); err != nil {
				return nil, 0, err
			}
		}
		results = append(results, item)
	}
	if err := iter.Err(); err != nil {
		return nil, 0, err
	}

	if err := tx.Commit(); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrTransactionFailed, err)
	}

	return results, total, nil
}

// Update applies field updates to all matching documents in a single transaction.
// Returns the number of updated documents.
func (qs QuerySet[T]) Update(fields SetFields) (int64, error) {
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
	txErr := RunInTransaction(qs.ctx, qs.db, func(tx *Tx) error {
		iter, err := tx.tx.Query(tx.ctx, col.meta.Name, q)
		if err != nil {
			return err
		}

		for iter.Next() {
			doc := new(T)
			if err := decodeIterRow(qs.db, iter.Bytes(), doc); err != nil {
				_ = iter.Close()
				return fmt.Errorf("decode: %w", err)
			}

			rv := reflect.ValueOf(doc).Elem()
			for fieldName, newVal := range fields {
				fv := rv.FieldByIndex(fieldInfos[fieldName].Index)
				if err := setFieldValue(fv, newVal, fieldName); err != nil {
					_ = iter.Close()
					return err
				}
			}
			if err := TxUpdate(tx, doc); err != nil {
				_ = iter.Close()
				return err
			}
			count++
		}
		iterErr := iter.Err()
		_ = iter.Close()
		if iterErr != nil {
			return iterErr
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
