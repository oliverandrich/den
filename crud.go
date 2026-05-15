package den

import (
	"context"
	"fmt"
	"reflect"
	"time"

	json "github.com/goccy/go-json"

	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/internal"
	"github.com/oliverandrich/den/validate"
	"github.com/oliverandrich/den/where"
)

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

// runPrePersistHooks runs the mutating hook chain, struct-tag constraint
// check, and custom Validate hook that every insert and update path
// executes before touching the backend. Pick the right BeforeInsert /
// BeforeUpdate branch via isInsert.
//
// Order is load-bearing:
//   - Mutating hooks run first so they can populate defaults, compute
//     derived fields, and normalize values before validation sees them.
//   - validate.Struct runs next — `validate:` struct-tag constraints
//     check the final post-hook state. Always-on, not opt-in: a doc with
//     constraint tags has those constraints enforced by Den itself.
//   - Custom Validator.Validate() runs last so it can perform cross-field
//     checks against that same post-hook state.
func runPrePersistHooks(ctx context.Context, doc any, isInsert bool) error {
	if isInsert {
		if err := runBeforeInsertHooks(ctx, doc); err != nil {
			return err
		}
	} else {
		if err := runBeforeUpdateHooks(ctx, doc); err != nil {
			return err
		}
	}
	if err := validate.Struct(doc); err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, err)
	}
	return runValidationHooks(ctx, doc)
}

// writeDocCore is the shared single-doc write body used by insertCore,
// updateCore, and cascade link-writes (saveSingleLinkedValue). Runs the
// full persist chain in canonical order: pre-persist hooks → revision
// handling → base field stamp → encode → Put → snapshot → after-hooks.
// The branch is driven by isInsert.
//
// Order is load-bearing: hooks fire first so they can populate defaults;
// revision handling next so encoded bytes carry the right _rev;
// setBaseFields stamps _id and timestamps last; encode captures the final
// post-mutation state.
//
// Does NOT handle cascade-write or transaction wrapping — those decisions
// pre-empt the write body and stay at the caller (updateCore wraps in a
// tx for revision atomicity; cascadeWriteLinks runs ahead of the parent's
// prepare chain).
func writeDocCore(ctx context.Context, db *DB, b ReadWriter, target any, col *collectionInfo, isInsert, ignoreRevision bool) error {
	tv := reflect.ValueOf(target).Elem()

	if err := runPrePersistHooks(ctx, target, isInsert); err != nil {
		return err
	}

	if col.settings.UseRevision {
		if isInsert {
			setRevision(tv, col.structInfo, newRevision())
		} else if err := checkAndUpdateRevision(ctx, db, b, col, tv, ignoreRevision); err != nil {
			return err
		}
	}

	setBaseFields(tv, col.structInfo, time.Now(), isInsert)

	data, err := db.encode(target)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	id := getID(tv, col.structInfo)
	if err := b.Put(ctx, col.meta.Name, id, data); err != nil {
		return err
	}
	captureSnapshot(data, target)

	if isInsert {
		return runAfterInsertHooks(ctx, target)
	}
	return runAfterUpdateHooks(ctx, target)
}

// FindByID retrieves a document by its ID. Returns ErrNotFound if no
// row matches.
//
// `den:"eager"`-tagged link fields on T are hydrated by default; pass
// WithoutFetchLinks to suppress hydration. Soft-deleted documents are
// returned: explicit-by-ID lookups bypass the soft-delete filter that
// QuerySet read terminals apply on filtered queries — callers can check
// Value.IsDeleted() to react.
//
// Top-level shorthand for `NewQuery[T](s).Where(where.Field("_id").Eq(id)).IncludeDeleted().First(ctx)`
// — discoverable next to Save / Delete / Refresh.
func FindByID[T any](ctx context.Context, s Scope, id string, opts ...CRUDOption) (*T, error) {
	return querySetFromOpts[T](s, []where.Condition{where.Field("_id").Eq(id)}, opts).IncludeDeleted().First(ctx)
}

// FindByIDs retrieves multiple documents by their IDs in a single query.
// Missing IDs are silently skipped. Order is not guaranteed.
//
// `den:"eager"`-tagged link fields on T are batch-resolved by default;
// pass WithoutFetchLinks to suppress hydration. Soft-deleted documents
// are returned (see FindByID for the rationale).
//
// Top-level shorthand for `NewQuery[T](s).Where(where.Field("_id").In(ids...)).IncludeDeleted().All(ctx)`.
func FindByIDs[T any](ctx context.Context, s Scope, ids []string, opts ...CRUDOption) ([]*T, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	anyIDs := make([]any, len(ids))
	for i, id := range ids {
		anyIDs[i] = id
	}
	return querySetFromOpts[T](s, []where.Condition{where.Field("_id").In(anyIDs...)}, opts).IncludeDeleted().All(ctx)
}

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

// SetFields is a map of field names (as they appear in the `json` struct
// tag) to new values for partial updates via QuerySet.UpdateOne,
// QuerySet.UpsertOne, and QuerySet.Update.
//
// Names are validated against the registered struct before the write
// transaction opens; an unknown name aborts the call without touching
// storage. Callers that want to validate names at application start can
// iterate Meta[T].Fields and compare against a known set.
type SetFields map[string]any

// querySetFromOpts translates the legacy CRUDOption surface (includeDeleted,
// fetchMode) into the equivalent QuerySet state. Internal helper for FindByID
// / FindByIDs, which still expose the option-based API.
func querySetFromOpts[T any](s Scope, conditions []where.Condition, opts []CRUDOption) QuerySet[T] {
	o := applyCRUDOpts(opts)
	qs := NewQuery[T](s, conditions...)
	if o.includeDeleted {
		qs = qs.IncludeDeleted()
	}
	switch crudFetchMode(o) { //nolint:exhaustive // fetchDefault matches the QuerySet's NewQuery default — no override needed.
	case fetchAll:
		qs = qs.WithFetchLinks()
	case fetchNone:
		qs = qs.WithoutFetchLinks()
	}
	return qs
}

// upsertResult bundles QuerySet.upsertOne's two return values so the
// runOnScope helper (which is single-valued over T) can carry both
// across the tx dispatch.
type upsertResult[T any] struct {
	doc      *T
	inserted bool
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
	includeDeleted bool,
) (*T, error) {
	qs := NewQuery[T](s, conditions...).Limit(2)
	if includeDeleted {
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

// encode and decode are the single JSON seam every storage write/read flows
// through. Kept as DB methods so every call site reads uniformly and so a
// future swap (e.g. encoding/json/v2 once it stabilises) lives in one place.
func (db *DB) encode(v any) ([]byte, error)    { return json.Marshal(v) }
func (db *DB) decode(data []byte, v any) error { return json.Unmarshal(data, v) }

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
