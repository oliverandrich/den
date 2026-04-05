package den

import (
	"context"
	"fmt"

	"github.com/oliverandrich/den/where"
)

// BackLinks finds all documents of type T that reference the given target ID
// through the specified link field. For example, BackLinks[House](db, "door", doorID)
// returns all Houses whose "door" link points to doorID.
func BackLinks[T any](ctx context.Context, db *DB, linkField string, targetID string) ([]*T, error) {
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, err
	}

	q := NewQuery[T](ctx, db, where.Field(linkField).Eq(targetID)).buildBackendQuery(col)

	iter, err := db.backend.Query(ctx, col.meta.Name, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	var results []*T
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
