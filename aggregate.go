package den

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// --- Cached projection/group mappings ---

type projField struct {
	sourceParts []string // pre-split JSON field path
	index       int      // field index in target struct
}

type groupField struct {
	tag   string // raw den tag value
	index int    // field index in target struct
}

var projCache sync.Map  // reflect.Type → []projField
var groupCache sync.Map // reflect.Type → []groupField

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

func getGroupMappings(elemType reflect.Type) []groupField {
	if cached, ok := groupCache.Load(elemType); ok {
		v, _ := cached.([]groupField)
		return v
	}

	var mappings []groupField
	for i := range elemType.NumField() {
		tag := elemType.Field(i).Tag.Get("den")
		if tag == "" {
			continue
		}
		mappings = append(mappings, groupField{tag: tag, index: i})
	}

	groupCache.Store(elemType, mappings)
	return mappings
}

// Avg returns the average of the given field across matching documents.
func (qs QuerySet[T]) Avg(field string) (float64, error) {
	return qs.aggregate(OpAvg, field)
}

// Sum returns the sum of the given field across matching documents.
func (qs QuerySet[T]) Sum(field string) (float64, error) {
	return qs.aggregate(OpSum, field)
}

// Min returns the minimum value of the given field across matching documents.
func (qs QuerySet[T]) Min(field string) (float64, error) {
	return qs.aggregate(OpMin, field)
}

// Max returns the maximum value of the given field across matching documents.
func (qs QuerySet[T]) Max(field string) (float64, error) {
	return qs.aggregate(OpMax, field)
}

func (qs QuerySet[T]) aggregate(op AggregateOp, field string) (float64, error) {
	col, err := collectionFor[T](qs.db)
	if err != nil {
		return 0, err
	}
	q := qs.buildBackendQuery(col)
	result, err := qs.db.backend.Aggregate(qs.ctx, col.meta.Name, op, field, q)
	if err != nil {
		return 0, err
	}
	if result == nil {
		return 0, nil
	}
	return *result, nil
}

// Project executes the query and decodes results into the projection type.
// Target must be a pointer to a slice of structs with json/den tags.
func (qs QuerySet[T]) Project(target any) error {
	col, err := collectionFor[T](qs.db)
	if err != nil {
		return err
	}

	q := qs.buildBackendQuery(col)
	iter, err := qs.db.backend.Query(qs.ctx, col.meta.Name, q)
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
	enc := qs.db.getEncoder()

	for iter.Next() {
		rawBytes := make([]byte, len(iter.Bytes()))
		copy(rawBytes, iter.Bytes())
		var docMap map[string]any
		if err := enc.Decode(rawBytes, &docMap); err != nil {
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

// GroupByBuilder allows specifying a group-by field.
type GroupByBuilder[T any] struct {
	qs    QuerySet[T]
	field string
}

// GroupBy starts a group-by aggregation on the given field.
func (qs QuerySet[T]) GroupBy(field string) GroupByBuilder[T] {
	return GroupByBuilder[T]{qs: qs, field: field}
}

// Into executes the group-by aggregation and maps results into the target slice.
func (gb GroupByBuilder[T]) Into(target any) error {
	col, err := collectionFor[T](gb.qs.db)
	if err != nil {
		return err
	}

	q := gb.qs.buildBackendQuery(col)
	iter, err := gb.qs.db.backend.Query(gb.qs.ctx, col.meta.Name, q)
	if err != nil {
		return err
	}
	defer func() { _ = iter.Close() }()

	enc := gb.qs.db.getEncoder()
	groups := make(map[any]*groupAcc)
	fieldParts := strings.Split(gb.field, ".")

	for iter.Next() {
		rawBytes := make([]byte, len(iter.Bytes()))
		copy(rawBytes, iter.Bytes())
		var docMap map[string]any
		if err := enc.Decode(rawBytes, &docMap); err != nil {
			return fmt.Errorf("decode for group-by: %w", err)
		}

		rawGroupVal := resolveMapFieldParts(docMap, fieldParts)
		// Ensure the group key is hashable (nested objects/arrays are not).
		groupVal := toHashableKey(rawGroupVal)
		acc, ok := groups[groupVal]
		if !ok {
			acc = &groupAcc{
				sums:   make(map[string]float64),
				counts: make(map[string]int64),
				mins:   make(map[string]float64),
				maxs:   make(map[string]float64),
			}
			groups[groupVal] = acc
		}
		acc.total++

		for k, v := range docMap {
			if f, ok := toFloat(v); ok {
				acc.sums[k] += f
				acc.counts[k]++
				if existing, exists := acc.mins[k]; !exists || f < existing {
					acc.mins[k] = f
				}
				if existing, exists := acc.maxs[k]; !exists || f > existing {
					acc.maxs[k] = f
				}
			}
		}
	}
	if err := iter.Err(); err != nil {
		return err
	}

	return mapGroupsToTarget(groups, target)
}

type groupAcc struct {
	sums   map[string]float64
	counts map[string]int64
	mins   map[string]float64
	maxs   map[string]float64
	total  int64
}

func mapGroupsToTarget(groups map[any]*groupAcc, target any) error {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("den: GroupBy.Into target must be a pointer to a slice")
	}

	sliceVal := rv.Elem()
	elemType := sliceVal.Type().Elem()
	mappings := getGroupMappings(elemType)

	for key, acc := range groups {
		elem := reflect.New(elemType).Elem()

		for _, m := range mappings {
			fv := elem.Field(m.index)

			switch {
			case m.tag == "group_key":
				if key != nil {
					rv := reflect.ValueOf(key)
					targetType := elemType.Field(m.index).Type
					if rv.Type().ConvertibleTo(targetType) {
						fv.Set(rv.Convert(targetType))
					}
				}
			case m.tag == "count":
				fv.SetInt(acc.total)
			case strings.HasPrefix(m.tag, "avg:"):
				fieldName := strings.TrimPrefix(m.tag, "avg:")
				if acc.counts[fieldName] > 0 {
					fv.SetFloat(acc.sums[fieldName] / float64(acc.counts[fieldName]))
				}
			case strings.HasPrefix(m.tag, "sum:"):
				fv.SetFloat(acc.sums[strings.TrimPrefix(m.tag, "sum:")])
			case strings.HasPrefix(m.tag, "min:"):
				fv.SetFloat(acc.mins[strings.TrimPrefix(m.tag, "min:")])
			case strings.HasPrefix(m.tag, "max:"):
				fv.SetFloat(acc.maxs[strings.TrimPrefix(m.tag, "max:")])
			}
		}

		sliceVal = reflect.Append(sliceVal, elem)
	}

	rv.Elem().Set(sliceVal)
	return nil
}

// toHashableKey converts a value to a type safe for use as a map key.
// JSON-decoded maps and slices are not comparable; we stringify them.
func toHashableKey(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice {
		return fmt.Sprintf("%v", v)
	}
	return v
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

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}
