package den

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/internal"
	"github.com/oliverandrich/den/where"
)

// Insert adds a new document to the database.
// If the document's ID is empty, a new ULID is generated.
// Options: WithLinkRule to cascade writes to linked documents.
func Insert[T any](ctx context.Context, db *DB, document *T, opts ...CRUDOption) error {
	return insertCore(ctx, db, db.backend, document, opts...)
}

func insertCore[T any](ctx context.Context, db *DB, b ReadWriter, document *T, opts ...CRUDOption) error {
	o := applyCRUDOpts(opts)
	col, err := collectionFor[T](db)
	if err != nil {
		return err
	}

	if o.linkRule == LinkWrite {
		if err := cascadeWriteLinks(ctx, db, b, document); err != nil {
			return err
		}
	}

	if err := runBeforeInsertHooks(ctx, document); err != nil {
		return err
	}

	rv := reflect.ValueOf(document).Elem()
	now := time.Now()
	setBaseFields(rv, col.structInfo, now, true)

	if col.settings.UseRevision {
		setRevision(rv, col.structInfo, newRevision())
	}

	data, err := db.encode(document)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	id := getID(rv, col.structInfo)

	if err := b.Put(ctx, col.meta.Name, id, data); err != nil {
		return err
	}

	captureSnapshot(data, document)
	return runAfterInsertHooks(ctx, document)
}

// FindByIDs retrieves multiple documents by their IDs in a single query.
// Missing IDs are silently skipped. Order is not guaranteed.
func FindByIDs[T any](ctx context.Context, db *DB, ids []string) ([]*T, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	anyIDs := make([]any, len(ids))
	for i, id := range ids {
		anyIDs[i] = id
	}

	return NewQuery[T](ctx, db, where.Field("_id").In(anyIDs...)).All()
}

// FindByID retrieves a document by its ID.
func FindByID[T any](ctx context.Context, db *DB, id string) (*T, error) {
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, err
	}

	data, err := db.backend.Get(ctx, col.meta.Name, id)
	if err != nil {
		return nil, err
	}

	result := new(T)
	if err := db.decode(data, result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	captureSnapshot(data, result)

	return result, nil
}

// Update updates an existing document in the database.
// Options: WithLinkRule to cascade writes, IgnoreRevision to skip conflict check.
func Update[T any](ctx context.Context, db *DB, document *T, opts ...CRUDOption) error {
	return updateCore(ctx, db, db.backend, document, opts...)
}

func updateCore[T any](ctx context.Context, db *DB, b ReadWriter, document *T, opts ...CRUDOption) error {
	o := applyCRUDOpts(opts)
	col, err := collectionFor[T](db)
	if err != nil {
		return err
	}

	if o.linkRule == LinkWrite {
		if err := cascadeWriteLinks(ctx, db, b, document); err != nil {
			return err
		}
	}

	rv := reflect.ValueOf(document).Elem()

	id := getID(rv, col.structInfo)
	if id == "" {
		return fmt.Errorf("den: cannot update document without ID")
	}

	if err := runBeforeUpdateHooks(ctx, document); err != nil {
		return err
	}

	if err := checkAndUpdateRevision(ctx, db, b, col, rv, o.ignoreRevision); err != nil {
		return err
	}

	now := time.Now()
	setBaseFields(rv, col.structInfo, now, false)

	data, err := db.encode(document)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	if err := b.Put(ctx, col.meta.Name, id, data); err != nil {
		return err
	}

	captureSnapshot(data, document)
	return runAfterUpdateHooks(ctx, document)
}

// Delete removes a document from the database.
// Options: WithLinkRule to cascade deletes to linked documents.
func Delete[T any](ctx context.Context, db *DB, document *T, opts ...CRUDOption) error {
	return deleteCore(ctx, db, db.backend, document, opts...)
}

func deleteCore[T any](ctx context.Context, db *DB, b ReadWriter, document *T, opts ...CRUDOption) error {
	o := applyCRUDOpts(opts)

	col, err := collectionFor[T](db)
	if err != nil {
		return err
	}

	rv := reflect.ValueOf(document).Elem()

	id := getID(rv, col.structInfo)
	if id == "" {
		return fmt.Errorf("den: cannot delete document without ID")
	}

	if err := runBeforeDeleteHooks(ctx, document); err != nil {
		return err
	}

	if o.linkRule == LinkDelete {
		if err := cascadeDeleteLinks(ctx, db, b, document); err != nil {
			return err
		}
	}

	if col.meta.HasSoftBase && !o.hardDelete {
		if err := softDelete(ctx, db, b, rv, document, col); err != nil {
			return err
		}
		return runAfterDeleteHooks(ctx, document)
	}

	if err := b.Delete(ctx, col.meta.Name, id); err != nil {
		return err
	}

	return runAfterDeleteHooks(ctx, document)
}

// Refresh re-reads a document from the database by its ID,
// overwriting all fields on the provided struct.
func Refresh[T any](ctx context.Context, db *DB, document *T) error {
	col, err := collectionFor[T](db)
	if err != nil {
		return err
	}

	id := getID(reflect.ValueOf(document).Elem(), col.structInfo)
	if id == "" {
		return fmt.Errorf("den: cannot refresh document without ID")
	}

	data, err := db.backend.Get(ctx, col.meta.Name, id)
	if err != nil {
		return err
	}

	if err := db.decode(data, document); err != nil {
		return err
	}
	captureSnapshot(data, document)
	return nil
}

// SetFields is a map of field names to new values for partial updates.
type SetFields map[string]any

// FindOneAndUpdate atomically finds the first matching document, applies
// the field updates, and returns the modified document.
// The find and replace are wrapped in a transaction for atomicity.
func FindOneAndUpdate[T any](ctx context.Context, db *DB, fields SetFields, conditions ...where.Condition) (*T, error) {
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, err
	}

	// Validate field names before starting the transaction
	for fieldName := range fields {
		if col.structInfo.FieldByName(fieldName) == nil {
			return nil, fmt.Errorf("den: field %q not found in %s", fieldName, col.meta.Name)
		}
	}

	qs := NewQuery[T](ctx, db, conditions...).Limit(1)
	q := qs.buildBackendQuery(col)

	var result *T
	txErr := RunInTransaction(ctx, db, func(tx *Tx) error {

		iter, err := tx.tx.Query(tx.ctx, col.meta.Name, q)
		if err != nil {
			return err
		}

		if !iter.Next() {
			err := iter.Err()
			_ = iter.Close()
			if err != nil {
				return err
			}
			return ErrNotFound
		}

		doc := new(T)
		rawBytes := make([]byte, len(iter.Bytes()))
		copy(rawBytes, iter.Bytes())
		_ = iter.Close()
		if err := db.decode(rawBytes, doc); err != nil {
			return fmt.Errorf("decode: %w", err)
		}
		captureSnapshot(rawBytes, doc)

		rv := reflect.ValueOf(doc).Elem()
		for fieldName, newVal := range fields {
			fi := col.structInfo.FieldByName(fieldName)
			if fi == nil {
				return fmt.Errorf("den: field %q not found in %s", fieldName, col.meta.Name)
			}
			fv := rv.FieldByIndex(fi.Index)
			if err := setFieldValue(fv, newVal, fieldName); err != nil {
				return err
			}
		}

		// Update within the transaction
		if err := TxUpdate(tx, doc); err != nil {
			return err
		}

		result = doc
		return nil
	})

	if txErr != nil {
		return nil, txErr
	}
	return result, nil
}

// InsertMany inserts multiple documents in a single transaction.
func InsertMany[T any](ctx context.Context, db *DB, documents []*T) error {
	if len(documents) == 0 {
		return nil
	}
	return RunInTransaction(ctx, db, func(tx *Tx) error {
		for _, doc := range documents {
			if err := TxInsert(tx, doc); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteMany deletes all documents matching the given conditions.
// Returns the number of deleted documents.
// All deletes run in one transaction.
func DeleteMany[T any](ctx context.Context, db *DB, conditions []where.Condition, opts ...CRUDOption) (int64, error) {
	col, err := collectionFor[T](db)
	if err != nil {
		return 0, err
	}

	qs := NewQuery[T](ctx, db, conditions...)
	q := qs.buildBackendQuery(col)

	var count int64
	txErr := RunInTransaction(ctx, db, func(tx *Tx) error {
		it, err := tx.tx.Query(tx.ctx, col.meta.Name, q)
		if err != nil {
			return err
		}
		defer func() { _ = it.Close() }()

		for it.Next() {
			rawBytes := make([]byte, len(it.Bytes()))
			copy(rawBytes, it.Bytes())
			doc := new(T)
			if err := db.decode(rawBytes, doc); err != nil {
				return fmt.Errorf("decode: %w", err)
			}
			if err := TxDelete(tx, doc, opts...); err != nil {
				return err
			}
			count++
		}
		return it.Err()
	})
	if txErr != nil {
		return 0, txErr
	}
	return count, nil
}

// setFieldValue sets a struct field to the given value, handling nil correctly.
func setFieldValue(fv reflect.Value, newVal any, fieldName string) error {
	if newVal == nil {
		fv.Set(reflect.Zero(fv.Type()))
		return nil
	}
	newRV := reflect.ValueOf(newVal)
	if newRV.Type() == fv.Type() {
		fv.Set(newRV)
		return nil
	}
	if !newRV.Type().ConvertibleTo(fv.Type()) {
		return fmt.Errorf("den: field %q: cannot assign %T to %s", fieldName, newVal, fv.Type())
	}
	fv.Set(newRV.Convert(fv.Type()))
	return nil
}

func (db *DB) encode(v any) ([]byte, error) {
	return db.getEncoder().Encode(v)
}

func (db *DB) decode(data []byte, v any) error {
	return db.getEncoder().Decode(data, v)
}

func (db *DB) getEncoder() Encoder {
	db.encoderOnce.Do(func() {
		db.encoder = db.backend.Encoder()
	})
	return db.encoder
}

func setBaseFields(v reflect.Value, info *internal.StructInfo, now time.Time, isInsert bool) {
	idField := info.FieldByName("_id")
	if idField != nil {
		fv := v.FieldByIndex(idField.Index)
		if fv.String() == "" {
			fv.SetString(document.NewID())
		}
	}

	if isInsert {
		createdField := info.FieldByName("_created_at")
		if createdField != nil {
			fv := v.FieldByIndex(createdField.Index)
			if fv.IsZero() {
				fv.Set(reflect.ValueOf(now))
			}
		}
	}

	updatedField := info.FieldByName("_updated_at")
	if updatedField != nil {
		v.FieldByIndex(updatedField.Index).Set(reflect.ValueOf(now))
	}
}

func getID(v reflect.Value, info *internal.StructInfo) string {
	idField := info.FieldByName("_id")
	if idField == nil {
		return ""
	}
	return v.FieldByIndex(idField.Index).String()
}
