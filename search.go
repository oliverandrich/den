package den

import (
	"context"
	"fmt"
)

// FTSProvider is an optional interface backends implement
// to support full-text search.
type FTSProvider interface {
	Search(ctx context.Context, collection string, query string, q *Query) (Iterator, error)
	EnsureFTS(ctx context.Context, collection string, fields []string) error
}

// Search performs a full-text search on the QuerySet.
func (qs QuerySet[T]) Search(queryText string) ([]*T, error) {
	col, err := collectionFor[T](qs.db)
	if err != nil {
		return nil, err
	}

	fts, ok := qs.db.backend.(FTSProvider)
	if !ok {
		return nil, fmt.Errorf("den: backend does not support full-text search")
	}

	q := qs.buildBackendQuery(col)

	iter, err := fts.Search(qs.ctx, col.meta.Name, queryText, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	var results []*T
	if qs.limitN > 0 {
		results = make([]*T, 0, qs.limitN)
	}
	for iter.Next() {
		doc := new(T)
		if err := decodeIterRow(qs.db, iter.Bytes(), doc); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}

		if qs.fetchLinks {
			if err := fetchAllLinksOnDoc(qs.ctx, qs.db, qs.db.backend, doc, qs.nestDepth); err != nil {
				return nil, fmt.Errorf("fetch links: %w", err)
			}
		}
		results = append(results, doc)
	}
	if err := iter.Err(); err != nil {
		return nil, err
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
