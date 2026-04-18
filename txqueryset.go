package den

import (
	"fmt"
	"slices"

	"github.com/oliverandrich/den/where"
)

// TxQuerySet is a lazy, immutable query builder bound to a transaction.
// Chain methods return copies; the query is only executed when a terminal
// method (All, First) is called. Use ForUpdate to acquire row-level locks
// that persist for the lifetime of the transaction.
//
// The zero value is not usable — always obtain a TxQuerySet via NewTxQuery
// inside a RunInTransaction closure. Calling terminal methods on a
// zero-value TxQuerySet panics because the transaction reference is nil.
type TxQuerySet[T any] struct {
	tx         *Tx
	conditions []where.Condition
	sortFields []SortEntry
	limitN     int
	skipN      int
	lock       *LockMode
	// err captures a deferred error from a chainable method (notably
	// ForUpdate with contradictory options) so it can surface on the
	// terminal All / First call — chainable methods can't return an error.
	err error
}

// NewTxQuery creates a new TxQuerySet bound to the given transaction.
// Conditions can optionally be passed directly. The query inherits the
// transaction's context and sees the in-transaction view of the data.
func NewTxQuery[T any](tx *Tx, conditions ...where.Condition) TxQuerySet[T] {
	qs := TxQuerySet[T]{tx: tx}
	if len(conditions) > 0 {
		qs.conditions = conditions
	}
	return qs
}

// Where adds filter conditions. Multiple calls are ANDed.
func (qs TxQuerySet[T]) Where(conditions ...where.Condition) TxQuerySet[T] {
	qs.conditions = append(slices.Clone(qs.conditions), conditions...)
	return qs
}

// Sort adds a sort criterion. Multiple calls define tie-breakers.
func (qs TxQuerySet[T]) Sort(field string, dir SortDirection) TxQuerySet[T] {
	qs.sortFields = append(slices.Clone(qs.sortFields), SortEntry{Field: field, Dir: dir})
	return qs
}

// Limit sets the maximum number of results.
func (qs TxQuerySet[T]) Limit(n int) TxQuerySet[T] {
	qs.limitN = n
	return qs
}

// Skip sets the number of results to skip.
func (qs TxQuerySet[T]) Skip(n int) TxQuerySet[T] {
	qs.skipN = n
	return qs
}

// ForUpdate acquires a row-level lock on every matching document, held until
// the enclosing transaction commits or rolls back. Other transactions that
// try to lock the same rows block until this transaction finishes.
//
// Pass SkipLocked to omit locked rows from the result set (queue-consumer
// pattern) or NoWait to fail immediately with ErrLocked when a row is held
// by another transaction. On SQLite these options are no-ops because
// IMMEDIATE transactions already serialize writers.
//
// Passing both SkipLocked and NoWait is a programmer error (PG allows only
// one); ForUpdate captures the error on the query set and surfaces it when
// a terminal method (All, First) runs.
func (qs TxQuerySet[T]) ForUpdate(opts ...LockOption) TxQuerySet[T] {
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

// All executes the query and returns all matching documents.
func (qs TxQuerySet[T]) All() ([]*T, error) {
	if qs.err != nil {
		return nil, qs.err
	}
	col, err := collectionFor[T](qs.tx.db)
	if err != nil {
		return nil, err
	}

	q := qs.buildBackendQuery(col)

	it, err := qs.tx.tx.Query(qs.tx.ctx, col.meta.Name, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = it.Close() }()

	var results []*T
	if qs.limitN > 0 {
		results = make([]*T, 0, qs.limitN)
	}
	for it.Next() {
		doc := new(T)
		if err := decodeIterRow(qs.tx.db, it.Bytes(), doc); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		results = append(results, doc)
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// First returns the first matching document. Returns ErrNotFound if none match.
func (qs TxQuerySet[T]) First() (*T, error) {
	results, err := qs.Limit(1).All()
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, ErrNotFound
	}
	return results[0], nil
}

func (qs TxQuerySet[T]) buildBackendQuery(col *collectionInfo) *Query {
	q := &Query{
		Collection: col.meta.Name,
		SortFields: qs.sortFields,
		LimitN:     qs.limitN,
		SkipN:      qs.skipN,
		Lock:       qs.lock,
	}
	q.Conditions = append(q.Conditions, qs.conditions...)
	return q
}
