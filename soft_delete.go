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

// SoftDeleteBy returns a CRUDOption that records an actor identifier (user
// ID, service name, etc.) on the document's DeletedBy field during a
// soft-delete. Silently ignored on the hard-delete path or on documents that
// do not embed document.SoftDelete — there is nowhere to store the value.
func SoftDeleteBy(actor string) CRUDOption {
	return func(o *crudOpts) {
		o.softDeleteBy = actor
	}
}

// SoftDeleteReason returns a CRUDOption that records a free-form reason on
// the document's DeleteReason field during a soft-delete. Silently ignored
// on the hard-delete path or on documents that do not embed
// document.SoftDelete.
func SoftDeleteReason(reason string) CRUDOption {
	return func(o *crudOpts) {
		o.softDeleteReason = reason
	}
}

// softDelete sets DeletedAt (and optionally DeletedBy / DeleteReason) on a
// document and replaces it in storage.
//
// For revisioned collections (UseRevision: true), soft-delete participates in
// the revision chain exactly like Update: the stored _rev is verified against
// the in-memory value, a fresh _rev is assigned, and the pair is written
// atomically. Concurrent writers holding the pre-delete revision therefore
// see ErrRevisionConflict on their next Update instead of silently clobbering
// DeletedAt. Pass IgnoreRevision to opt out.
func softDelete[T any](ctx context.Context, db *DB, b ReadWriter, rv reflect.Value, document *T, col *collectionInfo, o crudOpts) error {
	// When revision checking is active and we're not already in a
	// transaction, auto-wrap so the revision check (Get) and write (Put)
	// are atomic — preventing TOCTOU races on PostgreSQL where concurrent
	// pool connections can interleave. Mirrors updateCore's wrapping.
	if col.settings.UseRevision && !o.ignoreRevision {
		if backend, ok := b.(Backend); ok {
			return runInWriteTx(ctx, backend, func(tx Transaction) error {
				return softDelete(ctx, db, tx, rv, document, col, o)
			})
		}
	}

	if err := checkAndUpdateRevision(ctx, db, b, col, rv, o.ignoreRevision); err != nil {
		return err
	}

	now := time.Now()
	setSoftDeletedAt(rv, col.structInfo, &now)
	setSoftDeleteAuditFields(rv, col.structInfo, o.softDeleteBy, o.softDeleteReason)

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

// setSoftDeleteAuditFields assigns the optional audit metadata produced by
// SoftDeleteBy / SoftDeleteReason. Documents without the corresponding
// fields (hand-rolled soft-delete types that omit them) silently skip —
// consistent with the structural soft-delete detection.
func setSoftDeleteAuditFields(v reflect.Value, info *internal.StructInfo, by, reason string) {
	if by != "" {
		if f := info.FieldByName("_deleted_by"); f != nil {
			v.FieldByIndex(f.Index).SetString(by)
		}
	}
	if reason != "" {
		if f := info.FieldByName("_delete_reason"); f != nil {
			v.FieldByIndex(f.Index).SetString(reason)
		}
	}
}
