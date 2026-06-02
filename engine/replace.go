package engine

import (
	"context"
	"fmt"
	"reflect"

	"github.com/oliverandrich/den/internal/util"
)

// preserveServerFields copies every server-owned field present on the type
// (see serverOwnedFieldNames) from src into dst, skipping fields the type
// does not declare (FieldByName == nil). Both values must be addressable
// structs of the same registered type.
func preserveServerFields(info *util.StructInfo, dst, src reflect.Value) {
	for _, name := range serverOwnedFieldNames {
		if fi := info.FieldByName(name); fi != nil {
			dst.FieldByIndex(fi.Index).Set(src.FieldByIndex(fi.Index))
		}
	}
}

// PreserveServerFields copies Den's server-owned fields (_id, _created_at,
// _updated_at, _rev, and the soft-delete audit fields _deleted_at /
// _deleted_by / _delete_reason) from src onto dst, leaving every
// client-owned field untouched. The transient document.Tracked snapshot is
// not copied — Save recaptures it.
//
// It is the building block behind [Replace]; reach for it directly when you
// load and persist the existing record yourself. Returns ErrNotRegistered if
// T was never registered.
func PreserveServerFields[T any](db *DB, dst, src *T) error {
	col, err := collectionFor[T](db)
	if err != nil {
		return err
	}
	preserveServerFields(col.structInfo, reflect.ValueOf(dst).Elem(), reflect.ValueOf(src).Elem())
	return nil
}

// Replace performs a full-content replace (PUT semantics): the client-owned
// fields of fresh overwrite the stored row — fields omitted from fresh reset
// to their zero value — while Den's server-owned identity, audit, and
// soft-delete fields are preserved from the existing record (see
// [PreserveServerFields]).
//
// Replace is last-writer-wins: it adopts the stored _rev, so a revisioned
// type round-trips without conflict and Save bumps the revision. For
// optimistic concurrency, load the document, mutate it, and Save it directly
// instead. Replace does NOT resurrect soft-deleted documents — replacing a
// soft-deleted row leaves it soft-deleted.
//
// The load and the save run in one transaction (a new one when bound to a
// *DB, the caller's when bound to a *Tx). Fires the update hook chain
// (BeforeUpdate/BeforeSave → AfterUpdate/AfterSave). Returns ErrNotFound if
// no row matches fresh's _id, and ErrValidation if fresh has no _id.
func Replace[T any](ctx context.Context, s Scope, fresh *T, opts ...CRUDOption) error {
	col, err := collectionFor[T](s.db())
	if err != nil {
		return err
	}
	id := getID(reflect.ValueOf(fresh).Elem(), col.structInfo)
	if id == "" {
		return fmt.Errorf("%w: replace requires id", ErrValidation)
	}
	// Full-slice expressions (cap == len) force each append onto a fresh
	// backing array, so the two option lists never alias the caller's slice
	// or each other.
	findOpts := append(opts[:len(opts):len(opts)], WithoutFetchLinks())
	return runOnScopeVoid(ctx, s, func(tx *Tx) error {
		existing, err := FindByID[T](ctx, tx, id, findOpts...)
		if err != nil {
			return err // ErrNotFound propagates
		}
		preserveServerFields(col.structInfo,
			reflect.ValueOf(fresh).Elem(), reflect.ValueOf(existing).Elem())
		// FindByID already read the current _rev inside this transaction and
		// preserveServerFields copied it onto fresh; Save's own revision check
		// would be a redundant second read. Replace is last-writer-wins, so
		// skip it — Save still bumps _rev.
		saveOpts := append(opts[:len(opts):len(opts)], IgnoreRevision())
		return Save(ctx, tx, fresh, saveOpts...)
	})
}
