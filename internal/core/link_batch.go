package core

import (
	"context"
	"fmt"
	"reflect"

	"github.com/oliverandrich/den/where"
)

// batchResolveLinks resolves every link field on every parent in docs using
// one query per (depth level, target type) instead of one Get per row. IDs
// are deduplicated across parents so a hot target referenced by many parents
// is fetched once, and the same *Target pointer is shared into all matching
// parent slots. Nested links recurse per level, so N parents × L nesting
// levels × K target types costs at most L×K extra round-trips.
//
// Already-loaded links and empty IDs are skipped. Targets whose query returns
// no row leave their slots untouched (the link stays unloaded) — this is the
// batched analogue to the per-row path's ErrNotFound behavior where the
// caller decides how to handle dangling references via the returned error,
// except the batched path doesn't surface a dangling-link error (the IN
// query simply produces no row for that id). Callers that need strict
// dangling-link detection should stick to the streaming .Iter() path.
func batchResolveLinks[T any](ctx context.Context, db *DB, rw ReadWriter, docs []*T, depth int, mode fetchMode) error {
	if depth <= 0 || len(docs) == 0 || mode == fetchNone {
		return nil
	}
	return batchResolveLinksReflect(ctx, db, rw, reflect.ValueOf(docs), depth, mode)
}

// batchResolveLinksReflect is the reflective worker so recursive resolution
// at depth > 1 can operate on slices whose element type is only known via
// reflect.Type (the loaded-target slice of one level becomes the input of
// the next).
func batchResolveLinksReflect(ctx context.Context, db *DB, rw ReadWriter, docsVal reflect.Value, depth int, mode fetchMode) error {
	if depth <= 0 || docsVal.Len() == 0 || mode == fetchNone {
		return nil
	}
	// docsVal is []*T — Elem().Elem() peels off slice and pointer to reach T.
	elemType := docsVal.Type().Elem().Elem()
	for _, lf := range getLinkFields(elemType) {
		if lf.skipForMode(mode) {
			continue
		}
		if err := batchResolveField(ctx, db, rw, docsVal, lf, depth, mode); err != nil {
			return err
		}
	}
	return nil
}

// batchResolveField resolves a single link field across docsVal in one
// IN-query to the target collection. Shared IDs produce a single decode
// whose pointer is stored in every matching parent slot.
func batchResolveField(ctx context.Context, db *DB, rw ReadWriter, docsVal reflect.Value, lf linkFieldInfo, depth int, mode fetchMode) error {
	slotsByID := make(map[string][]reflect.Value)
	for i := range docsVal.Len() {
		docV := docsVal.Index(i).Elem() // *T → T (addressable)
		fv := docV.Field(lf.index)
		if lf.slice {
			for j := range fv.Len() {
				collectLinkSlot(fv.Index(j), lf, slotsByID)
			}
		} else {
			collectLinkSlot(fv, lf, slotsByID)
		}
	}
	if len(slotsByID) == 0 {
		return nil
	}

	col, err := collectionForType(db, lf.targetType)
	if err != nil {
		return err
	}

	ids := make([]any, 0, len(slotsByID))
	for id := range slotsByID {
		ids = append(ids, id)
	}
	q := &Query{
		Collection: col.meta.Name,
		Conditions: []where.Condition{where.Field("_id").In(ids...)},
	}
	iter, err := rw.Query(ctx, col.meta.Name, q)
	if err != nil {
		return err
	}
	// Collect the decoded targets so depth > 1 can recurse on them.
	loaded := reflect.MakeSlice(reflect.SliceOf(reflect.PointerTo(lf.targetType)), 0, len(slotsByID))
	matched := make(map[string]struct{}, len(slotsByID))
	for iter.Next() {
		if err := ctx.Err(); err != nil {
			_ = iter.Close()
			return err
		}
		id := iter.ID()
		slots, ok := slotsByID[id]
		if !ok {
			continue
		}
		target := reflect.New(lf.targetType)
		if err := decodeWithSnapshot(db, iter.Bytes(), target.Interface()); err != nil {
			_ = iter.Close()
			return fmt.Errorf("decode linked %s: %w", col.meta.Name, err)
		}
		for _, slot := range slots {
			slot.FieldByIndex(lf.valueIdx).Set(target)
			slot.FieldByIndex(lf.loadedIdx).SetBool(true)
		}
		matched[id] = struct{}{}
		loaded = reflect.Append(loaded, target)
	}
	if err := iter.Err(); err != nil {
		_ = iter.Close()
		return err
	}
	if err := iter.Close(); err != nil {
		return err
	}
	// A dangling link (ID referenced in a parent but with no corresponding
	// row in the target collection) surfaces as *DanglingLinkError. The
	// concrete type exposes (collection, id) for callers that need to act
	// on the broken link without parsing the message; it unwraps to
	// ErrNotFound so existing errors.Is(...) checks keep working.
	for id := range slotsByID {
		if _, ok := matched[id]; !ok {
			return &DanglingLinkError{Collection: col.meta.Name, ID: id}
		}
	}

	if depth > 1 && loaded.Len() > 0 {
		return batchResolveLinksReflect(ctx, db, rw, loaded, depth-1, mode)
	}
	return nil
}

// collectLinkSlot records linkVal in slotsByID under its ID, skipping empty
// IDs and already-loaded links. The slot is an addressable reflect.Value of
// the Link[T] struct so callers can write to Value / Loaded later.
func collectLinkSlot(linkVal reflect.Value, lf linkFieldInfo, slotsByID map[string][]reflect.Value) {
	id := linkVal.FieldByIndex(lf.idIdx).String()
	if id == "" {
		return
	}
	if linkVal.FieldByIndex(lf.loadedIdx).Bool() {
		return
	}
	slotsByID[id] = append(slotsByID[id], linkVal)
}
