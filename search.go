package den

import (
	"context"
)

// FTSSearcher is the read-side full-text search contract. Both backends and
// transactions implement it so [QuerySet.Search] honors the caller's scope:
// `NewQuery[T](db).Search(...)` reads committed state, while
// `NewQuery[T](tx).Search(...)` sees the tx's uncommitted writes (the FTS
// index is updated in-tx by triggers on SQLite and by tsvector + GIN under
// MVCC on PostgreSQL).
type FTSSearcher interface {
	Search(ctx context.Context, collection string, query string, q *Query) (Iterator, error)
}

// FTSProvider extends [FTSSearcher] with the registration-time setup hook.
// Backends implement the full interface; transactions implement only
// [FTSSearcher] because index/trigger creation is a one-time setup
// operation that does not belong on a transactional path.
type FTSProvider interface {
	FTSSearcher
	EnsureFTS(ctx context.Context, collection string, fields []string) error
}

// Search performs a full-text search on the QuerySet, honoring the
// QuerySet's scope: a tx-bound QuerySet sees the tx's uncommitted writes
// and rolls them back together with the rest of the tx, just like every
// other Den read. A *DB-bound QuerySet reads committed state.
//
// Returns [ErrFTSNotSupported] when the underlying scope does not implement
// [FTSSearcher] — either the backend has no FTS support, or the scope is a
// transaction on a backend whose tx side does not (no current backend has
// this asymmetry, but the contract leaves room for one).
func (qs QuerySet[T]) Search(ctx context.Context, queryText string) ([]*T, error) {
	if err := qs.preflight(); err != nil {
		return nil, err
	}
	db := qs.scope.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, err
	}

	fts, ok := qs.scope.readWriter().(FTSSearcher)
	if !ok {
		return nil, ErrFTSNotSupported
	}

	q := qs.buildBackendQuery(col)

	iter, err := fts.Search(ctx, col.meta.Name, queryText, q)
	if err != nil {
		return nil, err
	}
	results, err := drainIter[T](ctx, db, iter, qs.limitN)
	_ = iter.Close()
	if err != nil {
		return nil, err
	}

	if qs.fetchLinks {
		if err := batchResolveLinks(ctx, db, qs.scope.readWriter(), results, qs.nestDepth); err != nil {
			return nil, err
		}
	}
	return results, nil
}

// ensureFTSForCollection sets up FTS infrastructure during Register()
// if the backend supports it and the collection has FTS fields.
func ensureFTSForCollection(ctx context.Context, db *DB, meta CollectionMeta) error {
	fts, ok := db.backend.(FTSProvider)
	if !ok {
		return nil
	}

	var ftsFields []string
	for _, f := range meta.Fields {
		if f.FTS {
			ftsFields = append(ftsFields, f.Name)
		}
	}

	if len(ftsFields) == 0 {
		return nil
	}

	return fts.EnsureFTS(ctx, meta.Name, ftsFields)
}
