package den

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/oliverandrich/den/where"
)

// BackLinks finds all documents of type T that reference the given target ID
// through the specified link field. For example, BackLinks[House](ctx, db, "door", doorID)
// returns all Houses whose "door" link points to doorID. The scope parameter
// accepts either a *DB or a *Tx.
//
// linkField is the JSON tag on the holder's link field. Renaming the
// JSON tag silently breaks every BackLinks call against this collection.
// Prefer [BackLinksField] when the holder has exactly one Link[T] field
// for the target type — it's compile-checked on H and T and immune to
// JSON-tag renames. Use this string form to disambiguate when multiple
// Link[T] fields point at the same target type.
func BackLinks[T any](ctx context.Context, s Scope, linkField string, targetID string, opts ...CRUDOption) ([]*T, error) {
	db := s.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, err
	}

	rw := s.readWriter()
	q := NewQuery[T](db, where.Field(linkField).Eq(targetID)).buildBackendQuery(col)

	iter, err := rw.Query(ctx, col.meta.Name, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	results, err := drainIter[T](ctx, db, iter, 0)
	if err != nil {
		return nil, err
	}

	o := applyCRUDOpts(opts)
	if err := batchResolveLinks(ctx, db, rw, results, defaultNestingDepth, crudFetchMode(o)); err != nil {
		return nil, err
	}
	return results, nil
}

// BackLinksField is the typed alternative to BackLinks: it identifies
// the link relationship through the Go type parameters (H = the holder,
// T = the target) instead of a string field name. Internally the holder
// struct is walked once to find the unique Link[T] field; its JSON tag
// is then used for the underlying query.
//
//	houses, err := den.BackLinksField[House, Door](ctx, db, doorID)
//
// JSON-tag renames on the holder's link field are caught the next time
// BackLinksField runs, not silently ignored.
//
// Errors with a clear message in the cases the typed lookup deliberately
// rejects: when the holder has no Link[T] field at all, when it has more
// than one (use string-based BackLinks to disambiguate), or when the
// only matching fields are []Link[T] slices (use a manual
// where.Field(...).Contains(targetID) query — Eq doesn't match against
// array contents).
func BackLinksField[H any, T any](ctx context.Context, s Scope, targetID string, opts ...CRUDOption) ([]*H, error) {
	var holderZero H
	holderType := reflect.TypeOf(holderZero)
	var targetZero T
	targetType := reflect.TypeOf(targetZero)

	var sliceOnly bool
	var matches []linkFieldInfo
	for _, lf := range getLinkFields(holderType) {
		if lf.targetType != targetType {
			continue
		}
		if lf.slice {
			sliceOnly = true
			continue
		}
		matches = append(matches, lf)
	}

	switch len(matches) {
	case 0:
		if sliceOnly {
			return nil, fmt.Errorf(
				"den: BackLinksField: type %s only has []Link[%s] field(s); "+
					"slice-link backlinks are not supported (Eq vs array contents) — "+
					"use a manual query with where.Field(...).Contains(targetID)",
				holderType.Name(), targetType.Name(),
			)
		}
		return nil, fmt.Errorf("den: BackLinksField: type %s has no Link[%s] field",
			holderType.Name(), targetType.Name())
	case 1:
		// fall through
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			f := holderType.Field(m.index)
			name := parseJSONTagName(f.Tag.Get("json"))
			if name == "" {
				name = strings.ToLower(f.Name)
			}
			names[i] = name
		}
		return nil, fmt.Errorf(
			"den: BackLinksField: type %s has multiple Link[%s] fields (%s); "+
				"use BackLinks[%s] with the explicit field name to disambiguate",
			holderType.Name(), targetType.Name(), strings.Join(names, ", "), holderType.Name(),
		)
	}

	field := holderType.Field(matches[0].index)
	linkField := parseJSONTagName(field.Tag.Get("json"))
	if linkField == "" {
		linkField = strings.ToLower(field.Name)
	}
	return BackLinks[H](ctx, s, linkField, targetID, opts...)
}
