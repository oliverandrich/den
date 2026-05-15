package core

import (
	"context"
	"fmt"
	"reflect"
	"strings"
)

// FetchLink resolves a single named link field on a document. The scope
// parameter accepts either a *DB (read from the backend directly) or a *Tx
// (read from the enclosing transaction).
//
// The fieldName is the JSON tag on the parent's link field. Renaming
// the JSON tag silently breaks every FetchLink call against this
// collection. Prefer [FetchLinkField] when you can pass the typed
// link pointer directly — it's compile-checked.
func FetchLink[T any](ctx context.Context, s Scope, doc *T, fieldName string) error {
	return fetchLinkByName(ctx, s.db(), s.readWriter(), doc, fieldName, 1)
}

// FetchLinkField resolves the link by typed pointer instead of a
// stringly-named field on the parent. Use it when you have the
// Link[T] in hand directly — refactor-safe and immune to JSON-tag
// renames on the parent struct.
//
// No-op when the link's ID is empty (cascade-write input) or when
// Loaded is already true (idempotent — matches FetchLink).
func FetchLinkField[T any](ctx context.Context, s Scope, link *Link[T]) error {
	if link.ID == "" || link.Loaded {
		return nil
	}
	db := s.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return err
	}
	data, err := s.readWriter().Get(ctx, col.meta.Name, link.ID)
	if err != nil {
		return err
	}
	target := new(T)
	if err := decodeWithSnapshot(db, data, target); err != nil {
		return fmt.Errorf("decode linked %s: %w", col.meta.Name, err)
	}
	link.Value = target
	link.Loaded = true
	return nil
}

// FetchAllLinks resolves the direct link fields on doc — single-level, the
// loaded targets' own links stay untouched. See FetchLink for the scope
// semantics. The eager / lazy tag on each field is ignored; calling
// FetchAllLinks is itself the explicit ask for hydration.
//
// For transitive hydration use a QuerySet terminal (All / AllWithCount /
// First / Search), which honors WithNestingDepth. Internally this routes
// through the same batched resolver — a one-element batch — so depth
// recursion is available; FetchAllLinks fixes it at one hop because the
// API has no place to thread a depth knob.
func FetchAllLinks[T any](ctx context.Context, s Scope, doc *T) error {
	return batchResolveLinks(ctx, s.db(), s.readWriter(), []*T{doc}, 1, fetchAll)
}

// fetchLinkByName resolves one named link field. The rw parameter carries
// the ReadWriter that should service the actual Get for the linked document:
// pass a Transaction from inside an open iterator so the read reuses the
// same connection (avoids pool exhaustion) and, on stricter isolation
// levels, the same snapshot. Fall back to db.backend when no tx is open.
func fetchLinkByName(ctx context.Context, db *DB, rw ReadWriter, doc any, fieldName string, depth int) error {
	if depth <= 0 {
		return nil
	}

	v := reflect.ValueOf(doc).Elem()
	t := v.Type()

	for _, lf := range getLinkFields(t) {
		field := t.Field(lf.index)
		name := parseJSONTagName(field.Tag.Get("json"))
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		if name != fieldName {
			continue
		}

		fv := v.Field(lf.index)
		if lf.slice {
			for j := range fv.Len() {
				if err := resolveSingleLink(ctx, db, rw, fv.Index(j), lf); err != nil {
					return err
				}
			}
			return nil
		}
		return resolveSingleLink(ctx, db, rw, fv, lf)
	}
	return fmt.Errorf("den: link field %q not found", fieldName)
}

func resolveSingleLink(ctx context.Context, db *DB, rw ReadWriter, linkVal reflect.Value, lf linkFieldInfo) error {
	idField := linkVal.FieldByIndex(lf.idIdx)
	if idField.String() == "" {
		return nil
	}

	loadedField := linkVal.FieldByIndex(lf.loadedIdx)
	if loadedField.Bool() {
		return nil // already loaded
	}

	id := idField.String()
	valueField := linkVal.FieldByIndex(lf.valueIdx)

	// Determine the target type (the T in Link[T])
	targetType := valueField.Type().Elem() // *T → T

	// Look up the collection for this type (respects custom CollectionName)
	col, err := collectionForType(db, targetType)
	if err != nil {
		return err
	}
	colName := col.meta.Name

	// Fetch the document via the caller-supplied ReadWriter so that,
	// when called from inside an iterator's TX, the read reuses the same
	// connection instead of grabbing a second one from the pool.
	data, err := rw.Get(ctx, colName, id)
	if err != nil {
		return err
	}

	// Decode into a new instance of T
	target := reflect.New(targetType)
	if err := decodeWithSnapshot(db, data, target.Interface()); err != nil {
		return fmt.Errorf("decode linked %s: %w", colName, err)
	}

	valueField.Set(target)
	loadedField.SetBool(true)
	return nil
}
