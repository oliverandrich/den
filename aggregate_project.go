package den

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

type projField struct {
	sourceParts []string // pre-split JSON field path
	index       int      // field index in target struct
}

var projCache sync.Map // reflect.Type → []projField

func getProjMappings(elemType reflect.Type) []projField {
	if cached, ok := projCache.Load(elemType); ok {
		v, _ := cached.([]projField)
		return v
	}

	var mappings []projField
	for i := range elemType.NumField() {
		field := elemType.Field(i)

		denTag := field.Tag.Get("den")
		var fieldName string
		if after, ok := strings.CutPrefix(denTag, "from:"); ok {
			fieldName = after
		} else {
			jsonTag := field.Tag.Get("json")
			if jsonTag == "" || jsonTag == "-" {
				continue
			}
			fieldName, _, _ = strings.Cut(jsonTag, ",")
		}

		mappings = append(mappings, projField{sourceParts: strings.Split(fieldName, "."), index: i})
	}

	projCache.Store(elemType, mappings)
	return mappings
}

// Project executes the query and decodes results into the projection type.
// Target must be a pointer to a slice of structs with json/den tags.
func (qs QuerySet[T]) Project(ctx context.Context, target any) error {
	if err := qs.preflight(); err != nil {
		return err
	}
	col, err := collectionFor[T](qs.scope.db())
	if err != nil {
		return err
	}

	q := qs.buildBackendQuery(col)
	iter, err := qs.scope.readWriter().Query(ctx, col.meta.Name, q)
	if err != nil {
		return err
	}
	defer func() { _ = iter.Close() }()

	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("den: Project target must be a pointer to a slice")
	}

	sliceVal := rv.Elem()
	elemType := sliceVal.Type().Elem()
	db := qs.scope.db()

	for iter.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var docMap map[string]any
		if err := db.decode(iter.Bytes(), &docMap); err != nil {
			return fmt.Errorf("decode for projection: %w", err)
		}

		elem := reflect.New(elemType).Elem()
		mapProjection(docMap, elem, elemType)
		sliceVal = reflect.Append(sliceVal, elem)
	}
	if err := iter.Err(); err != nil {
		return err
	}

	rv.Elem().Set(sliceVal)
	return nil
}

func mapProjection(doc map[string]any, elem reflect.Value, elemType reflect.Type) {
	for _, m := range getProjMappings(elemType) {
		val := resolveMapFieldParts(doc, m.sourceParts)
		if val == nil {
			continue
		}

		fv := elem.Field(m.index)
		rv := reflect.ValueOf(val)
		if rv.Type().ConvertibleTo(fv.Type()) {
			fv.Set(rv.Convert(fv.Type()))
		}
	}
}

func resolveMapFieldParts(doc map[string]any, parts []string) any {
	var current any = doc
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[part]
	}
	return current
}
