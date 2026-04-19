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
//
// The scope parameter accepts either a *DB (operating outside a transaction)
// or a *Tx (operating inside RunInTransaction). See the Scope interface.
func Insert[T any](ctx context.Context, s Scope, document *T, opts ...CRUDOption) error {
	return insertCore(ctx, s.db(), s.readWriter(), document, opts...)
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

	// Mutating hooks run first so they can populate defaults, compute
	// derived fields, and normalize values before validation sees them.
	if err := runBeforeInsertHooks(ctx, document); err != nil {
		return err
	}

	// Tag-based validation runs after the mutating hooks so declarative
	// constraints are checked against the final document state.
	if db.tagValidator != nil {
		if err := db.tagValidator(document); err != nil {
			return fmt.Errorf("%w: %w", ErrValidation, err)
		}
	}

	// Custom Validator.Validate() runs last so it can perform cross-field
	// checks against the same post-hook state.
	if err := runValidationHooks(ctx, document); err != nil {
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
func FindByIDs[T any](ctx context.Context, s Scope, ids []string) ([]*T, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	anyIDs := make([]any, len(ids))
	for i, id := range ids {
		anyIDs[i] = id
	}

	db := s.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, err
	}

	q := NewQuery[T](db, where.Field("_id").In(anyIDs...)).buildBackendQuery(col)
	iter, err := s.readWriter().Query(ctx, col.meta.Name, q)
	if err != nil {
		return nil, err
	}
	results, err := drainIter[T](db, iter, len(ids))
	_ = iter.Close()
	return results, err
}

// FindByID retrieves a document by its ID.
func FindByID[T any](ctx context.Context, s Scope, id string) (*T, error) {
	db := s.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, err
	}

	data, err := s.readWriter().Get(ctx, col.meta.Name, id)
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
func Update[T any](ctx context.Context, s Scope, document *T, opts ...CRUDOption) error {
	return updateCore(ctx, s.db(), s.readWriter(), document, opts...)
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

	rv := reflect.ValueOf(document).Elem()

	id := getID(rv, col.structInfo)
	if id == "" {
		return fmt.Errorf("%w: cannot update document without ID", ErrValidation)
	}

	// Mutating hooks run first so they can populate defaults, compute
	// derived fields, and normalize values before validation sees them.
	if err := runBeforeUpdateHooks(ctx, document); err != nil {
		return err
	}

	// Tag-based validation runs after the mutating hooks so declarative
	// constraints are checked against the final document state.
	if db.tagValidator != nil {
		if err := db.tagValidator(document); err != nil {
			return fmt.Errorf("%w: %w", ErrValidation, err)
		}
	}

	// Custom Validator.Validate() runs last so it can perform cross-field
	// checks against the same post-hook state.
	if err := runValidationHooks(ctx, document); err != nil {
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
func Delete[T any](ctx context.Context, s Scope, document *T, opts ...CRUDOption) error {
	return deleteCore(ctx, s.db(), s.readWriter(), document, opts...)
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
		return fmt.Errorf("%w: cannot delete document without ID", ErrValidation)
	}

	if err := runBeforeDeleteHooks(ctx, document); err != nil {
		return err
	}

	if o.linkRule == LinkDelete {
		if err := cascadeDeleteLinks(ctx, db, b, document); err != nil {
			return err
		}
	}

	if col.meta.HasSoftDelete && !o.hardDelete {
		if err := softDelete(ctx, db, b, rv, document, col); err != nil {
			return err
		}
		return runAfterDeleteHooks(ctx, document)
	}

	if err := b.Delete(ctx, col.meta.Name, id); err != nil {
		return err
	}

	// Hard-delete cascade: drop the bytes behind any document.Attachment
	// fields. Best-effort — orphan bytes are logged, not returned.
	db.cleanupAttachments(ctx, rv)

	return runAfterDeleteHooks(ctx, document)
}

// Refresh re-reads a document from the database by its ID,
// overwriting all fields on the provided struct.
func Refresh[T any](ctx context.Context, s Scope, document *T) error {
	db := s.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return err
	}

	id := getID(reflect.ValueOf(document).Elem(), col.structInfo)
	if id == "" {
		return fmt.Errorf("den: cannot refresh document without ID")
	}

	data, err := s.readWriter().Get(ctx, col.meta.Name, id)
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
//
// When scope is a *DB, a new transaction is opened; when scope is a *Tx,
// the operation runs inline in the caller's transaction.
func FindOneAndUpdate[T any](ctx context.Context, s Scope, fields SetFields, conditions ...where.Condition) (*T, error) {
	db := s.db()
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

	qs := NewQuery[T](db, conditions...).Limit(1)
	q := qs.buildBackendQuery(col)

	body := func(tx *Tx) (*T, error) {
		iter, err := tx.tx.Query(ctx, col.meta.Name, q)
		if err != nil {
			return nil, err
		}

		if !iter.Next() {
			err := iter.Err()
			_ = iter.Close()
			if err != nil {
				return nil, err
			}
			return nil, ErrNotFound
		}

		doc := new(T)
		iterBytes := iter.Bytes()
		_ = iter.Close()
		if err := decodeIterRow(db, iterBytes, doc); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}

		rv := reflect.ValueOf(doc).Elem()
		for fieldName, newVal := range fields {
			fi := col.structInfo.FieldByName(fieldName)
			if fi == nil {
				return nil, fmt.Errorf("den: field %q not found in %s", fieldName, col.meta.Name)
			}
			fv := rv.FieldByIndex(fi.Index)
			if err := setFieldValue(fv, newVal, fieldName); err != nil {
				return nil, err
			}
		}

		if err := Update(ctx, tx, doc); err != nil {
			return nil, err
		}
		return doc, nil
	}

	if tx, ok := s.(*Tx); ok {
		return body(tx)
	}

	var result *T
	txErr := RunInTransaction(ctx, db, func(tx *Tx) error {
		doc, err := body(tx)
		if err != nil {
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
//
// When scope is a *DB, a new transaction is opened for the batch; when
// scope is a *Tx, the inserts run inline in the caller's transaction.
func InsertMany[T any](ctx context.Context, s Scope, documents []*T) error {
	if len(documents) == 0 {
		return nil
	}
	body := func(tx *Tx) error {
		for _, doc := range documents {
			if err := Insert(ctx, tx, doc); err != nil {
				return err
			}
		}
		return nil
	}
	if tx, ok := s.(*Tx); ok {
		return body(tx)
	}
	return RunInTransaction(ctx, s.db(), body)
}

// DeleteMany deletes all documents matching the given conditions.
// Returns the number of deleted documents.
//
// When scope is a *DB, all deletes run in one new transaction; when scope
// is a *Tx, the deletes run inline in the caller's transaction.
func DeleteMany[T any](ctx context.Context, s Scope, conditions []where.Condition, opts ...CRUDOption) (int64, error) {
	db := s.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return 0, err
	}

	qs := NewQuery[T](db, conditions...)
	q := qs.buildBackendQuery(col)

	var count int64
	body := func(tx *Tx) error {
		it, err := tx.tx.Query(ctx, col.meta.Name, q)
		if err != nil {
			return err
		}
		defer func() { _ = it.Close() }()

		for it.Next() {
			doc := new(T)
			if err := decodeIterRow(db, it.Bytes(), doc); err != nil {
				return fmt.Errorf("decode: %w", err)
			}
			if err := Delete(ctx, tx, doc, opts...); err != nil {
				return err
			}
			count++
		}
		return it.Err()
	}

	if tx, ok := s.(*Tx); ok {
		if err := body(tx); err != nil {
			return 0, err
		}
		return count, nil
	}
	txErr := RunInTransaction(ctx, db, body)
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
	if idField := info.BaseID; idField != nil {
		fv := v.FieldByIndex(idField.Index)
		if fv.String() == "" {
			fv.SetString(document.NewID())
		}
	}

	nowVal := reflect.ValueOf(now)

	if isInsert {
		if createdField := info.BaseCreatedAt; createdField != nil {
			fv := v.FieldByIndex(createdField.Index)
			if fv.IsZero() {
				fv.Set(nowVal)
			}
		}
	}

	if updatedField := info.BaseUpdatedAt; updatedField != nil {
		v.FieldByIndex(updatedField.Index).Set(nowVal)
	}
}

func getID(v reflect.Value, info *internal.StructInfo) string {
	idField := info.BaseID
	if idField == nil {
		return ""
	}
	return v.FieldByIndex(idField.Index).String()
}
