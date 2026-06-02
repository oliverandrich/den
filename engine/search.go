package engine

import (
	"context"

	"github.com/oliverandrich/den/search"
)

// Search performs a literal-terms full-text search on the QuerySet: the raw
// term is treated as a set of words ANDed together, with FTS5 operators and
// punctuation neutralised (via [search.LiteralFTS5]), giving plainto_tsquery-
// like semantics on every backend. This makes raw user input safe to pass
// straight through on both SQLite and PostgreSQL. A blank/whitespace-only
// term returns no rows without touching the backend.
//
// For raw backend-native query syntax (FTS5 query expressions on SQLite) use
// [QuerySet.SearchRaw]. Both honor the QuerySet's scope identically.
func (qs QuerySet[T]) Search(ctx context.Context, term string) ([]*T, error) {
	queryText := search.LiteralFTS5(term)
	if queryText == "" { // all-blank term has no literal tokens to match
		return []*T{}, nil
	}
	return qs.searchRaw(ctx, queryText)
}

// SearchRaw performs a full-text search passing the term straight to the
// backend's native FTS query mechanism. On SQLite this is an FTS5 query
// expression (operators, column filters, prefix * all honored — and raw user
// input unsafe); on PostgreSQL it is still plainto_tsquery, which normalises
// the term and ignores operators, so the "raw" extra power is effectively
// SQLite-only. Most callers want [QuerySet.Search], which neutralises
// operators for safe user-supplied input identically on both backends.
func (qs QuerySet[T]) SearchRaw(ctx context.Context, term string) ([]*T, error) {
	return qs.searchRaw(ctx, term)
}

// searchRaw is the shared FTS terminal: it dispatches the already-prepared
// query text to the scope's [FTSSearcher], honoring the QuerySet's scope —
// a tx-bound QuerySet sees the tx's uncommitted writes and rolls them back
// together with the rest of the tx, just like every other Den read; a
// *DB-bound QuerySet reads committed state.
//
// Returns [ErrFTSNotSupported] when the underlying scope does not implement
// [FTSSearcher] — either the backend has no FTS support, or the scope is a
// transaction on a backend whose tx side does not (no current backend has
// this asymmetry, but the contract leaves room for one).
func (qs QuerySet[T]) searchRaw(ctx context.Context, queryText string) ([]*T, error) {
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

	if qs.shouldHydrate() {
		if err := batchResolveLinks(ctx, db, qs.scope.readWriter(), results, qs.nestDepth, qs.fetchMode); err != nil {
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
