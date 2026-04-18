package den

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/oliverandrich/den/internal"
)

// HardDelete permanently removes a document from storage, bypassing soft-delete.
// Hooks and link cascade are applied as with regular Delete.
func HardDelete[T any](ctx context.Context, db *DB, document *T, opts ...CRUDOption) error {
	opts = append(opts, hardDeleteOption())
	return deleteCore(ctx, db, db.backend, document, opts...)
}

func hardDeleteOption() CRUDOption {
	return func(o *crudOpts) {
		o.hardDelete = true
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
