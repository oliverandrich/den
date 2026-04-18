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
func (qs QuerySet[T]) Search(ctx context.Context, queryText string) ([]*T, error) {
	col, err := collectionFor[T](qs.db)
	if err != nil {
		return nil, err
	}

	fts, ok := qs.db.backend.(FTSProvider)
	if !ok {
		return nil, ErrFTSNotSupported
	}

	q := qs.buildBackendQuery(col)

	iter, err := fts.Search(ctx, col.meta.Name, queryText, q)
	if err != nil {
		return nil, err
	}
	results, err := drainIter[T](qs.db, iter, qs.limitN)
	_ = iter.Close()
	if err != nil {
		return nil, err
	}

	if qs.fetchLinks {
		if err := batchResolveLinks(ctx, qs.db, qs.db.backend, results, qs.nestDepth); err != nil {
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
