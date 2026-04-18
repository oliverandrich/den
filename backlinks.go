package den

import (
	"context"

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

	q := NewQuery[T](db, where.Field(linkField).Eq(targetID)).buildBackendQuery(col)

	iter, err := db.backend.Query(ctx, col.meta.Name, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	return drainIter[T](ctx, iter, db, db.backend, false, 0, 0)
}
