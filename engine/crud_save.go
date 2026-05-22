package engine

import (
	"context"
	"fmt"
	"reflect"
)

// Save inserts the document if its ID is empty, otherwise updates it.
// The single doc-in-hand persistence entry point: callers don't pick
// branches, Save inspects the ID and routes accordingly.
//
// Empty-ID docs follow the insert path (ULID assigned, BeforeInsert
// hooks fire). ID-bearing docs follow the update path (revision check,
// BeforeUpdate hooks fire). Exactly one branch runs.
//
// Options pass through to whichever underlying path runs.
func Save[T any](ctx context.Context, s Scope, document *T, opts ...CRUDOption) error {
	col, err := collectionFor[T](s.db())
	if err != nil {
		return err
	}
	id := getID(reflect.ValueOf(document).Elem(), col.structInfo)
	if id == "" {
		return insertCore(ctx, s.db(), s.readWriter(), document, opts...)
	}
	return updateCore(ctx, s.db(), s.readWriter(), document, opts...)
}

// SaveAll persists every doc in docs by routing each through Save: empty-ID
// docs take the Insert path, ID-bearing docs take the Update path. Mixed
// batches are supported — every doc gets the right branch.
//
// All saves run inside a single transaction when bound to a *DB; when
// bound to a *Tx they run inline in the caller's transaction. Fail-fast:
// any per-doc error rolls back the batch.
//
// Empty input is a no-op (returns nil without opening a transaction).
func SaveAll[T any](ctx context.Context, s Scope, docs []*T, opts ...CRUDOption) error {
	if len(docs) == 0 {
		return nil
	}
	return runOnScopeVoid(ctx, s, func(tx *Tx) error {
		for i, doc := range docs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := Save(ctx, tx, doc, opts...); err != nil {
				return fmt.Errorf("den: save failed at index %d: %w", i, err)
			}
		}
		return nil
	})
}

func insertCore[T any](ctx context.Context, db *DB, b ReadWriter, document *T, opts ...CRUDOption) error {
	o := applyCRUDOpts(opts)

	// Cascade stays ahead of the prep chain so BeforeInsert hooks observe
	// the linked children's IDs that cascade just populated on the parent.
	if o.linkRule == LinkWrite {
		if err := cascadeWriteLinks(ctx, db, b, document); err != nil {
			return err
		}
	}
	col, err := collectionFor[T](db)
	if err != nil {
		return err
	}
	return writeDocCore(ctx, db, b, document, col, true, false)
}

func updateCore[T any](ctx context.Context, db *DB, b ReadWriter, document *T, opts ...CRUDOption) error {
	o := applyCRUDOpts(opts)
	col, err := collectionFor[T](db)
	if err != nil {
		return err
	}

	// When revision checking is active and we're not already in a
	// transaction, auto-wrap in a transaction so the revision check (Get)
	// and write (Put) are atomic — preventing TOCTOU races on PostgreSQL
	// where concurrent pool connections can interleave.
	if col.settings.UseRevision && !o.ignoreRevision {
		if backend, ok := b.(Backend); ok {
			return runInWriteTx(ctx, backend, func(tx Transaction) error {
				return updateCore(ctx, db, tx, document, opts...)
			})
		}
	}

	if o.linkRule == LinkWrite {
		if err := cascadeWriteLinks(ctx, db, b, document); err != nil {
			return err
		}
	}

	if getID(reflect.ValueOf(document).Elem(), col.structInfo) == "" {
		return fmt.Errorf("%w: cannot update document without ID", ErrValidation)
	}

	return writeDocCore(ctx, db, b, document, col, false, o.ignoreRevision)
}
