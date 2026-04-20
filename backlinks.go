package den

import (
	"context"

	"github.com/oliverandrich/den/where"
)

// BackLinks finds all documents of type T that reference the given target ID
// through the specified link field. For example, BackLinks[House](ctx, db, "door", doorID)
// returns all Houses whose "door" link points to doorID. The scope parameter
// accepts either a *DB or a *Tx.
func BackLinks[T any](ctx context.Context, s Scope, linkField string, targetID string) ([]*T, error) {
	db := s.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, err
	}

	q := NewQuery[T](db, where.Field(linkField).Eq(targetID)).buildBackendQuery(col)

	iter, err := s.readWriter().Query(ctx, col.meta.Name, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	return drainIter[T](ctx, db, iter, 0)
}
