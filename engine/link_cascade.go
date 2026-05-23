package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

// cascadeWriteLinks saves all linked documents that have a Value set.
func cascadeWriteLinks(ctx context.Context, db *DB, b ReadWriter, doc any) error {
	return forEachLinkField(ctx, doc, func(elem reflect.Value, lf linkFieldInfo) error {
		return saveSingleLinkedValue(ctx, db, b, elem, lf)
	})
}

// cascadeDeleteLinks deletes the immediate link targets of doc. The walk is
// strictly single-level: each target is removed via the backend directly
// without re-entering Delete() / deleteCore() / cascadeDeleteLinks, so the
// targets' own links are left intact. Callers that need transitive deletion
// must drive it themselves.
//
// The crudOpts are forwarded so HardDelete(), SoftDeleteBy() and
// SoftDeleteReason() all propagate: hard-deletes on the parent
// hard-delete the linked targets too (even when they embed
// document.SoftDelete), and the audit options reach the cascade soft
// path via the shared deleteDocCore.
func cascadeDeleteLinks(ctx context.Context, db *DB, b ReadWriter, doc any, o crudOpts) error {
	return forEachLinkField(ctx, doc, func(elem reflect.Value, lf linkFieldInfo) error {
		return deleteSingleLinkedValue(ctx, db, b, elem, lf, o)
	})
}

func saveSingleLinkedValue(ctx context.Context, db *DB, b ReadWriter, linkVal reflect.Value, lf linkFieldInfo) error {
	valueField := linkVal.FieldByIndex(lf.valueIdx)
	if valueField.IsNil() {
		return nil
	}

	target := valueField.Interface()
	col, err := collectionForType(db, valueField.Type().Elem())
	if err != nil {
		return err
	}

	tv := reflect.ValueOf(target).Elem()
	isInsert := getID(tv, col.structInfo) == ""

	// IgnoreRevision is parent-only: cascade children always enforce their
	// own revision contract, independent of how the parent was opted out.
	if err := writeDocCore(ctx, db, b, target, col, isInsert, false); err != nil {
		return err
	}

	// Propagate the (possibly newly generated) child ID back into the
	// parent's Link slot. Runs after writeDocCore, but still before the
	// parent's BeforeInsert/Update fires — cascadeWriteLinks is invoked
	// ahead of the parent's prepare chain.
	if isInsert {
		linkVal.FieldByIndex(lf.idIdx).SetString(getID(tv, col.structInfo))
	}
	return nil
}

// deleteSingleLinkedValue removes one link target: loads the stored doc,
// fires BeforeDelete / AfterDelete hooks, and writes the backend delete
// (or the soft-delete flip). The backend call is terminal for this target —
// no cascade re-enters from here, so the target's own links stay untouched.
//
// The hard/soft branch mirrors deleteCore: a SoftDelete-embedding target
// goes the soft path UNLESS the caller passed HardDelete(), in which case
// the row is physically removed and only BeforeDelete / AfterDelete fire
// (the soft-only hook pair is skipped, matching the direct-delete
// semantics).
func deleteSingleLinkedValue(ctx context.Context, db *DB, b ReadWriter, linkVal reflect.Value, lf linkFieldInfo, o crudOpts) error {
	idField := linkVal.FieldByIndex(lf.idIdx)
	if idField.String() == "" {
		return nil
	}

	targetType := linkVal.FieldByIndex(lf.valueIdx).Type().Elem()
	col, err := collectionForType(db, targetType)
	if err != nil {
		return err
	}

	// Load the linked document so hooks see the persisted state and
	// softDelete can read the current revision.
	data, err := b.Get(ctx, col.meta.Name, idField.String())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil // already deleted
		}
		return err
	}

	docPtr := reflect.New(targetType)
	doc := docPtr.Interface()
	if err := db.decode(data, doc); err != nil {
		return fmt.Errorf("decode linked %s: %w", col.meta.Name, err)
	}

	if err := runBeforeDeleteHooks(ctx, doc); err != nil {
		return err
	}

	return deleteDocCore(ctx, db, b, doc, col, o)
}
