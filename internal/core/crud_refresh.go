package core

import (
	"context"
	"fmt"
	"reflect"
)

// Refresh re-reads a document from the database by its ID,
// overwriting all fields on the provided struct.
//
// `den:"eager"`-tagged link fields on T are hydrated by default; pass
// WithoutFetchLinks to suppress hydration.
func Refresh[T any](ctx context.Context, s Scope, document *T, opts ...CRUDOption) error {
	db := s.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return err
	}

	id := getID(reflect.ValueOf(document).Elem(), col.structInfo)
	if id == "" {
		return fmt.Errorf("den: cannot refresh document without ID")
	}

	rw := s.readWriter()
	data, err := rw.Get(ctx, col.meta.Name, id)
	if err != nil {
		return err
	}

	if err := decodeWithSnapshot(db, data, document); err != nil {
		return err
	}

	o := applyCRUDOpts(opts)
	return batchResolveLinks(ctx, db, rw, []*T{document}, defaultNestingDepth, crudFetchMode(o))
}

// RefreshAll re-reads every doc in docs by routing each through Refresh.
// All refreshes run inside a single transaction when bound to a *DB; when
// bound to a *Tx they run inline in the caller's transaction. Fail-fast:
// any per-doc error rolls back the batch.
//
// Empty input is a no-op.
func RefreshAll[T any](ctx context.Context, s Scope, docs []*T, opts ...CRUDOption) error {
	if len(docs) == 0 {
		return nil
	}
	return runOnScopeVoid(ctx, s, func(tx *Tx) error {
		for i, doc := range docs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := Refresh(ctx, tx, doc, opts...); err != nil {
				return fmt.Errorf("den: refresh failed at index %d: %w", i, err)
			}
		}
		return nil
	})
}
