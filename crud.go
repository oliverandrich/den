package den

import (
	"context"
	"errors"
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

	// Cascade stays ahead of the prep chain so BeforeInsert hooks observe
	// the linked children's IDs that cascade just populated on the parent.
	if o.linkRule == LinkWrite {
		if err := cascadeWriteLinks(ctx, db, b, document); err != nil {
			return err
		}
	}

	id, data, col, err := prepareInsert(ctx, db, document)
	if err != nil {
		return err
	}
	return commitInsert(ctx, b, col, document, id, data)
}

// runPrePersistHooks runs the mutating hook, tag-validator, and Validate
// chain that every insert and update path executes before touching the
// backend. Shared by prepareInsert, updateCore, and saveSingleLinkedValue so
// the chain lives in exactly one place. Pick the right BeforeInsert /
// BeforeUpdate branch via isInsert — downstream steps (setBaseFields,
// revision handling, encode, Put) differ and remain at each call site.
//
// Order is load-bearing:
//   - Mutating hooks run first so they can populate defaults, compute
//     derived fields, and normalize values before validation sees them.
//   - Tag-based validation runs next so declarative constraints are checked
//     against the final post-hook state.
//   - Custom Validator.Validate() runs last so it can perform cross-field
//     checks against that same post-hook state.
func runPrePersistHooks(ctx context.Context, db *DB, doc any, isInsert bool) error {
	if isInsert {
		if err := runBeforeInsertHooks(ctx, doc); err != nil {
			return err
		}
	} else {
		if err := runBeforeUpdateHooks(ctx, doc); err != nil {
			return err
		}
	}
	if db.tagValidator != nil {
		if err := db.tagValidator(doc); err != nil {
			return fmt.Errorf("%w: %w", ErrValidation, err)
		}
	}
	return runValidationHooks(ctx, doc)
}

// prepareInsert runs the mutating hooks, validation, base-field assignment,
// revision assignment, and encoding — every part of an insert that can happen
// without touching the backend. It returns the encoded bytes ready for a
// subsequent Put, plus the id and collection info the caller needs to perform
// that Put and fire After-hooks.
//
// Cascade is NOT part of prepareInsert because cascade writes through the
// backend; callers that opt into LinkWrite run cascadeWriteLinks themselves
// before calling prepareInsert, so BeforeInsert sees populated link IDs.
func prepareInsert[T any](ctx context.Context, db *DB, document *T) (string, []byte, *collectionInfo, error) {
	col, err := collectionFor[T](db)
	if err != nil {
		return "", nil, nil, err
	}

	if err := runPrePersistHooks(ctx, db, document, true); err != nil {
		return "", nil, nil, err
	}

	rv := reflect.ValueOf(document).Elem()
	setBaseFields(rv, col.structInfo, time.Now(), true)
	if col.settings.UseRevision {
		setRevision(rv, col.structInfo, newRevision())
	}

	data, err := db.encode(document)
	if err != nil {
		return "", nil, nil, fmt.Errorf("encode: %w", err)
	}
	return getID(rv, col.structInfo), data, col, nil
}

// commitInsert performs the backend Put, snapshots the written bytes for
// change tracking, and fires the After-hooks. The encoded data must already
// reflect every mutation the document will carry: cascade and every Before /
// Validate hook must have run before commitInsert. Mutating document between
// the prepare and commit calls desyncs the in-memory doc from the persisted
// bytes — data is frozen at prepare time.
func commitInsert[T any](ctx context.Context, b ReadWriter, col *collectionInfo, document *T, id string, data []byte) error {
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
	results, err := drainIter[T](ctx, db, iter, len(ids))
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

	if err := runPrePersistHooks(ctx, db, document, false); err != nil {
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

// SetFields is a map of field names (as they appear in the `json` struct
// tag) to new values for partial updates used by FindOneAndUpdate,
// FindOneAndUpsert, and QuerySet.Update.
//
// Names are validated against the registered struct before the write
// transaction opens; an unknown name aborts the call without touching
// storage. Callers that want to validate names at application start can
// iterate Meta[T].Fields and compare against a known set.
type SetFields map[string]any

// FindOneAndUpdate atomically finds the single matching document, applies
// the field updates, and returns the modified document.
// The find and replace are wrapped in a transaction for atomicity.
//
// Returns ErrNotFound if no document matches and ErrMultipleMatches if more
// than one matches — the conditions must identify the document uniquely.
//
// When scope is a *DB, a new transaction is opened; when scope is a *Tx,
// the operation runs inline in the caller's transaction.
//
// Pass IncludeSoftDeleted to consider soft-deleted documents in the match.
func FindOneAndUpdate[T any](ctx context.Context, s Scope, fields SetFields, conditions []where.Condition, opts ...CRUDOption) (*T, error) {
	db := s.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, err
	}

	o := applyCRUDOpts(opts)

	body := func(tx *Tx) (*T, error) {
		doc, err := findOneStrict[T](ctx, tx, conditions, o.includeSoftDeleted)
		if err != nil {
			return nil, err
		}

		rv := reflect.ValueOf(doc).Elem()
		if err := applySetFields(rv, col, fields); err != nil {
			return nil, err
		}

		if err := Update(ctx, tx, doc); err != nil {
			return nil, err
		}
		return doc, nil
	}

	return runOnScope(ctx, s, body)
}

// FindOneAndUpsert atomically finds the single document matching conditions
// and applies fields, or inserts a new document built from defaults with
// fields applied on top. The third return value reports which path ran:
// true means a new document was inserted, false means an existing one was
// updated.
//
// Conditions must identify the document uniquely: ErrMultipleMatches is
// returned if more than one document matches. The match-and-write happen in
// a single transaction so the upsert is atomic against itself; concurrent
// upserts on the same missing row rely on a unique constraint to fail one of
// the inserters with ErrDuplicate — there is no internal retry, and no row
// lock is taken on the lookup (an absent row cannot be locked).
//
// On the miss path the defaults pointer is mutated by Insert (ID, CreatedAt,
// UpdatedAt are populated) and returned as the result. Callers reusing a
// shared defaults template across upserts should pass a fresh value each
// call — a stale ID would otherwise be carried into the next Insert.
//
// Hooks follow the standard Insert / Update order. Exactly one path runs:
//   - Hit:  BeforeUpdate → BeforeSave → tag-validation → Validate → write → AfterUpdate → AfterSave
//   - Miss: BeforeInsert → BeforeSave → tag-validation → Validate → write → AfterInsert → AfterSave
//
// Soft-deleted matches are ignored by default — pass IncludeSoftDeleted to
// have them satisfy the lookup. DeletedAt is left as-is when an existing
// soft-deleted document is updated; clear it explicitly via fields if the
// caller wants to resurrect.
//
// When scope is a *DB, a new transaction is opened; when scope is a *Tx, the
// operation runs inline in the caller's transaction.
func FindOneAndUpsert[T any](
	ctx context.Context,
	s Scope,
	defaults *T,
	fields SetFields,
	conditions []where.Condition,
	opts ...CRUDOption,
) (*T, bool, error) {
	db := s.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, false, err
	}

	o := applyCRUDOpts(opts)

	body := func(tx *Tx) (upsertResult[T], error) {
		existing, err := findOneStrict[T](ctx, tx, conditions, o.includeSoftDeleted)
		switch {
		case err == nil:
			rv := reflect.ValueOf(existing).Elem()
			if err := applySetFields(rv, col, fields); err != nil {
				return upsertResult[T]{}, err
			}
			if err := Update(ctx, tx, existing); err != nil {
				return upsertResult[T]{}, err
			}
			return upsertResult[T]{doc: existing, inserted: false}, nil
		case errors.Is(err, ErrNotFound):
			rv := reflect.ValueOf(defaults).Elem()
			if err := applySetFields(rv, col, fields); err != nil {
				return upsertResult[T]{}, err
			}
			if err := Insert(ctx, tx, defaults); err != nil {
				return upsertResult[T]{}, err
			}
			return upsertResult[T]{doc: defaults, inserted: true}, nil
		default:
			return upsertResult[T]{}, err
		}
	}

	res, err := runOnScope(ctx, s, body)
	if err != nil {
		return nil, false, err
	}
	return res.doc, res.inserted, nil
}

// upsertResult bundles FindOneAndUpsert's two return values so the helper
// runOnScope (which is single-valued over T) can carry both across the tx
// dispatch.
type upsertResult[T any] struct {
	doc      *T
	inserted bool
}

// PreValidate makes InsertMany run the full insert hook + validation chain
// on every document before opening the write transaction. If any document
// fails, no writes are attempted.
//
// BeforeInsert / BeforeSave / Validate fire exactly once per document; the
// pre-pass caches the encoded bytes and the in-transaction commit only runs
// Put + AfterInsert / AfterSave. (When combined with WithLinkRule(LinkWrite),
// cascade must run inside the tx so the hook chain runs again there — the
// optimization does not apply to that combination.)
func PreValidate() CRUDOption {
	return func(o *crudOpts) {
		o.preValidate = true
	}
}

// ContinueOnError makes InsertMany write each document in its own short-lived
// transaction instead of failing the whole batch on the first error. The
// returned error (if any) is an *InsertManyError listing per-document
// failures by input index.
//
// Loses cross-document atomicity — successful inserts are committed even when
// later ones fail. Returns ErrIncompatibleScope when called with a *Tx scope;
// returns ErrIncompatibleOptions when combined with PreValidate (each doc
// gets its own transaction, so a global pre-pass would leave the per-doc
// guarantee ill-defined).
func ContinueOnError() CRUDOption {
	return func(o *crudOpts) {
		o.continueOnError = true
	}
}

// InsertMany inserts multiple documents in a single transaction.
//
// When scope is a *DB, a new transaction is opened for the batch; when
// scope is a *Tx, the inserts run inline in the caller's transaction.
//
// See PreValidate and ContinueOnError for the available behavioral options.
func InsertMany[T any](ctx context.Context, s Scope, documents []*T, opts ...CRUDOption) error {
	if len(documents) == 0 {
		return nil
	}
	o := applyCRUDOpts(opts)

	if o.continueOnError && o.preValidate {
		return fmt.Errorf("%w: PreValidate and ContinueOnError", ErrIncompatibleOptions)
	}

	if o.continueOnError {
		if _, ok := s.(*Tx); ok {
			return fmt.Errorf("%w: ContinueOnError requires *DB", ErrIncompatibleScope)
		}
		return insertManyContinueOnError(ctx, s.db(), documents, opts)
	}

	if o.preValidate && o.linkRule != LinkWrite {
		// Optimized path: cache encoded bytes between the pre-pass and the
		// in-tx commit so the hook + validate + encode chain runs once.
		db := s.db()
		prepared := make([]preparedInsert[T], 0, len(documents))
		for i, doc := range documents {
			if err := ctx.Err(); err != nil {
				return err
			}
			id, data, col, err := prepareInsert(ctx, db, doc)
			if err != nil {
				return fmt.Errorf("den: pre-validation failed at index %d: %w", i, err)
			}
			prepared = append(prepared, preparedInsert[T]{doc: doc, id: id, data: data, col: col})
		}
		body := func(tx *Tx) error {
			for i, p := range prepared {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := commitInsert(ctx, tx.tx, p.col, p.doc, p.id, p.data); err != nil {
					return fmt.Errorf("den: insert failed at index %d: %w", i, err)
				}
			}
			return nil
		}
		return runOnScopeVoid(ctx, s, body)
	}

	if o.preValidate {
		// LinkWrite path: cascade must run inside the tx and before hooks
		// so they see populated link IDs. The pre-pass only catches early
		// validation failures; the full Insert chain (including hooks)
		// runs again in the tx.
		db := s.db()
		for i, doc := range documents {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, _, _, err := prepareInsert(ctx, db, doc); err != nil {
				return fmt.Errorf("den: pre-validation failed at index %d: %w", i, err)
			}
		}
	}

	body := func(tx *Tx) error {
		for i, doc := range documents {
			if err := Insert(ctx, tx, doc, opts...); err != nil {
				return fmt.Errorf("den: insert failed at index %d: %w", i, err)
			}
		}
		return nil
	}
	return runOnScopeVoid(ctx, s, body)
}

// preparedInsert carries the output of a prepareInsert call between the
// PreValidate pre-pass and the commit pass inside the write transaction.
type preparedInsert[T any] struct {
	doc  *T
	id   string
	data []byte
	col  *collectionInfo
}

func insertManyContinueOnError[T any](ctx context.Context, db *DB, docs []*T, opts []CRUDOption) error {
	var failures []InsertFailure
	for i, doc := range docs {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := RunInTransaction(ctx, db, func(tx *Tx) error {
			return Insert(ctx, tx, doc, opts...)
		})
		if err != nil {
			failures = append(failures, InsertFailure{Index: i, Err: err})
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return &InsertManyError{Failures: failures}
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
			if err := ctx.Err(); err != nil {
				return err
			}
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

	if err := runOnScopeVoid(ctx, s, body); err != nil {
		return 0, err
	}
	return count, nil
}

// findOneStrict loads exactly one document matching conditions. Returns
// ErrNotFound if none match, ErrMultipleMatches if more than one matches.
//
// Limit(2) lets the backend stop after the second row — enough to detect
// non-uniqueness without scanning the full match set.
func findOneStrict[T any](
	ctx context.Context,
	s Scope,
	conditions []where.Condition,
	includeSoftDeleted bool,
) (*T, error) {
	qs := NewQuery[T](s, conditions...).Limit(2)
	if includeSoftDeleted {
		qs = qs.IncludeDeleted()
	}
	results, err := qs.All(ctx)
	if err != nil {
		return nil, err
	}
	switch len(results) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return results[0], nil
	default:
		return nil, ErrMultipleMatches
	}
}

// applySetFields applies a SetFields map to a struct value, validating that
// each named field exists on the collection's struct.
func applySetFields(rv reflect.Value, col *collectionInfo, fields SetFields) error {
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
	return nil
}

// validateSetFields checks that every field name in fields exists on the
// collection's struct. Shared by callers that need pre-transaction validation
// (QuerySet.Update) — the in-tx applySetFields re-validates as it goes, so
// within the tx this step is not required.
func validateSetFields(col *collectionInfo, fields SetFields) error {
	for fieldName := range fields {
		if col.structInfo.FieldByName(fieldName) == nil {
			return fmt.Errorf("den: field %q not found in %s", fieldName, col.meta.Name)
		}
	}
	return nil
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
