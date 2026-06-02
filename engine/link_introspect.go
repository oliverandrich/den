package engine

import (
	"reflect"
)

// LinkFields enumerates the Link[T] / []Link[T] relation fields of the
// registered document type T. Use it to validate or allowlist expandable
// relations (e.g. an `?expand=` API) without re-implementing reflection over
// Link[T].
//
// Returns ErrNotRegistered if T is not registered. A link whose target type
// is itself unregistered is still reported, with an empty TargetCollection
// (the field is valid; only its collection name is unknown).
func LinkFields[T any](db *DB) ([]LinkFieldMeta, error) {
	if _, err := collectionFor[T](db); err != nil {
		return nil, err
	}
	t := reflect.TypeFor[T]()
	fields := getLinkFields(t)
	metas := make([]LinkFieldMeta, 0, len(fields))
	for _, lf := range fields {
		f := t.Field(lf.index)
		meta := LinkFieldMeta{
			JSONName:   linkFieldJSONName(t, lf),
			GoName:     f.Name,
			Slice:      lf.slice,
			Eager:      lf.eager,
			TargetType: lf.targetType,
		}
		if col, err := collectionForType(db, lf.targetType); err == nil {
			meta.TargetCollection = col.meta.Name
		}
		metas = append(metas, meta)
	}
	return metas, nil
}
