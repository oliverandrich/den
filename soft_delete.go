package den

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/oliverandrich/den/internal"
)

// HardDelete returns a CRUDOption that makes Delete permanently remove a
// document from storage, bypassing soft-delete. Hooks and link cascade are
// still applied. Compose with other CRUDOptions such as WithLinkRule:
//
//	den.Delete(ctx, db, doc, den.HardDelete())
//	den.Delete(ctx, db, doc, den.HardDelete(), den.WithLinkRule(den.LinkDelete))
func HardDelete() CRUDOption {
	return func(o *crudOpts) {
		o.hardDelete = true
	}
}

// IncludeSoftDeleted returns a CRUDOption that makes lookup-style operations
// consider soft-deleted documents. Currently honored by FindOneAndUpdate and
// FindOneAndUpsert: without it, soft-deleted matches are skipped (Upsert then
// inserts a fresh document); with it, the soft-deleted document is updated in
// place and DeletedAt is left untouched.
func IncludeSoftDeleted() CRUDOption {
	return func(o *crudOpts) {
		o.includeSoftDeleted = true
	}
}

// softDelete sets DeletedAt on a document and replaces it in storage.
func softDelete[T any](ctx context.Context, db *DB, b ReadWriter, rv reflect.Value, document *T, col *collectionInfo) error {
	now := time.Now()
	setSoftDeletedAt(rv, col.structInfo, &now)

	data, err := db.encode(document)
	if err != nil {
		return fmt.Errorf("encode soft delete: %w", err)
	}

	id := getID(rv, col.structInfo)

	if err := b.Put(ctx, col.meta.Name, id, data); err != nil {
		return err
	}
	captureSnapshot(data, document)
	return nil
}

func setSoftDeletedAt(v reflect.Value, info *internal.StructInfo, t *time.Time) {
	field := info.BaseDeletedAt
	if field == nil {
		return
	}
	fv := v.FieldByIndex(field.Index)
	if t == nil {
		fv.Set(reflect.Zero(fv.Type()))
	} else {
		fv.Set(reflect.ValueOf(t))
	}
}
