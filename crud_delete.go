package den

import (
	"context"
	"fmt"
	"reflect"
)

// Delete removes a document from the database.
// Options: WithLinkRule to cascade deletes to linked documents.
func Delete[T any](ctx context.Context, s Scope, document *T, opts ...CRUDOption) error {
	return deleteCore(ctx, s.db(), s.readWriter(), document, opts...)
}

// DeleteAll removes every doc in docs by routing each through Delete.
// All deletes run inside a single transaction when bound to a *DB; when
// bound to a *Tx they run inline in the caller's transaction. Fail-fast:
// any per-doc error rolls back the batch.
//
// Empty input is a no-op.
func DeleteAll[T any](ctx context.Context, s Scope, docs []*T, opts ...CRUDOption) error {
	if len(docs) == 0 {
		return nil
	}
	return runOnScopeVoid(ctx, s, func(tx *Tx) error {
		for i, doc := range docs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := Delete(ctx, tx, doc, opts...); err != nil {
				return fmt.Errorf("den: delete failed at index %d: %w", i, err)
			}
		}
		return nil
	})
}

func deleteCore[T any](ctx context.Context, db *DB, b ReadWriter, document *T, opts ...CRUDOption) error {
	o := applyCRUDOpts(opts)

	col, err := collectionFor[T](db)
	if err != nil {
		return err
	}

	rv := reflect.ValueOf(document).Elem()
	if getID(rv, col.structInfo) == "" {
		return fmt.Errorf("%w: cannot delete document without ID", ErrValidation)
	}

	if err := runBeforeDeleteHooks(ctx, document); err != nil {
		return err
	}

	if o.linkRule == LinkDelete {
		if err := cascadeDeleteLinks(ctx, db, b, document, o); err != nil {
			return err
		}
	}

	return deleteDocCore(ctx, db, b, document, col, o)
}

// deleteDocCore performs the post-BeforeDelete delete chain on a loaded
// document: branches on soft vs hard, fires the soft-only hook pair when
// applicable, and runs AfterDelete. The caller owns BeforeDelete and any
// cascade-delete of links — cascade pre-empts this body and is
// single-level by design.
func deleteDocCore(ctx context.Context, db *DB, b ReadWriter, doc any, col *collectionInfo, o crudOpts) error {
	rv := reflect.ValueOf(doc).Elem()

	if col.meta.HasSoftDelete && !o.hardDelete {
		if err := runBeforeSoftDeleteHooks(ctx, doc); err != nil {
			return err
		}
		if err := softDelete(ctx, db, b, rv, doc, col, o); err != nil {
			return err
		}
		if err := runAfterSoftDeleteHooks(ctx, doc); err != nil {
			return err
		}
		return runAfterDeleteHooks(ctx, doc)
	}

	if err := db.preflightAttachments(rv); err != nil {
		return err
	}
	if err := b.Delete(ctx, col.meta.Name, getID(rv, col.structInfo)); err != nil {
		return err
	}
	// Best-effort: drop the bytes behind document.Attachment fields.
	// Remote Storage failures are logged, not returned.
	db.cleanupAttachments(ctx, rv)
	return runAfterDeleteHooks(ctx, doc)
}
