package den

import (
	"context"
)

// FTSProvider is an optional interface backends implement
// to support full-text search.
type FTSProvider interface {
	Search(ctx context.Context, collection string, query string, q *Query) (Iterator, error)
	EnsureFTS(ctx context.Context, collection string, fields []string) error
}

// Search performs a full-text search on the QuerySet.
//
// Search always routes through the backend's FTSProvider implementation
// because full-text indexes live at the backend level (FTS5 virtual tables
// on SQLite, tsvector columns on PostgreSQL). Tx-bound QuerySets are
// accepted but delegate the FTS query to the DB's backend rather than the
// transaction — FTS reads do not need the in-transaction snapshot.
func (qs QuerySet[T]) Search(ctx context.Context, queryText string) ([]*T, error) {
	if err := qs.preflight(); err != nil {
		return nil, err
	}
	db := qs.scope.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, err
	}

	fts, ok := db.backend.(FTSProvider)
	if !ok {
		return nil, ErrFTSNotSupported
	}

	q := qs.buildBackendQuery(col)

	iter, err := fts.Search(ctx, col.meta.Name, queryText, q)
	if err != nil {
		return nil, err
	}
	results, err := drainIter[T](db, iter, qs.limitN)
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
