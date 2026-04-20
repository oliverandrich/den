package den

import (
	"context"
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
func (qs QuerySet[T]) Avg(ctx context.Context, field string) (float64, error) {
	return qs.aggregate(ctx, OpAvg, field)
}

// Sum returns the sum of the given field across matching documents.
func (qs QuerySet[T]) Sum(ctx context.Context, field string) (float64, error) {
	return qs.aggregate(ctx, OpSum, field)
}

// Min returns the minimum value of the given field across matching documents.
func (qs QuerySet[T]) Min(ctx context.Context, field string) (float64, error) {
	return qs.aggregate(ctx, OpMin, field)
}

// Max returns the maximum value of the given field across matching documents.
func (qs QuerySet[T]) Max(ctx context.Context, field string) (float64, error) {
	return qs.aggregate(ctx, OpMax, field)
}

func (qs QuerySet[T]) aggregate(ctx context.Context, op AggregateOp, field string) (float64, error) {
	if err := qs.preflight(); err != nil {
		return 0, err
	}
	col, err := collectionFor[T](qs.scope.db())
	if err != nil {
		return 0, err
	}
	q := qs.buildBackendQuery(col)
	result, err := qs.scope.readWriter().Aggregate(ctx, col.meta.Name, op, field, q)
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
	enc := qs.scope.db().getEncoder()

	for iter.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
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
// The query is pushed down to the database as a SQL GROUP BY statement.
func (gb GroupByBuilder[T]) Into(ctx context.Context, target any) error {
	if err := gb.qs.preflight(); err != nil {
		return err
	}
	col, err := collectionFor[T](gb.qs.scope.db())
	if err != nil {
		return err
	}

	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("den: GroupBy.Into target must be a pointer to a slice")
	}

	sliceVal := rv.Elem()
	elemType := sliceVal.Type().Elem()
	mappings := getGroupMappings(elemType)

	// Build the list of aggregate expressions from the target struct's den tags.
	aggs, aggIndices := buildAggsFromMappings(mappings)

	q := gb.qs.buildBackendQuery(col)
	rows, err := gb.qs.scope.readWriter().GroupBy(ctx, col.meta.Name, gb.field, aggs, q)
	if err != nil {
		return err
	}

	for _, row := range rows {
		elem := reflect.New(elemType).Elem()

		for _, m := range mappings {
			fv := elem.Field(m.index)

			if m.tag == "group_key" {
				targetType := elemType.Field(m.index).Type
				kv := reflect.ValueOf(row.Key)
				if kv.Type().ConvertibleTo(targetType) {
					fv.Set(kv.Convert(targetType))
				}
			} else if idx, ok := aggIndices[m.tag]; ok {
				if m.tag == "count" {
					fv.SetInt(int64(row.Values[idx]))
				} else {
					fv.SetFloat(row.Values[idx])
				}
			}
		}

		sliceVal = reflect.Append(sliceVal, elem)
	}

	rv.Elem().Set(sliceVal)
	return nil
}

// buildAggsFromMappings converts den struct tags into GroupByAgg descriptors.
// Returns the aggs and a map from tag → index in the Values slice.
func buildAggsFromMappings(mappings []groupField) ([]GroupByAgg, map[string]int) {
	var aggs []GroupByAgg
	indices := make(map[string]int)

	for _, m := range mappings {
		switch {
		case m.tag == "group_key":
			continue
		case m.tag == "count":
			indices[m.tag] = len(aggs)
			aggs = append(aggs, GroupByAgg{Op: OpCount})
		case strings.HasPrefix(m.tag, "avg:"):
			indices[m.tag] = len(aggs)
			aggs = append(aggs, GroupByAgg{Op: OpAvg, Field: strings.TrimPrefix(m.tag, "avg:")})
		case strings.HasPrefix(m.tag, "sum:"):
			indices[m.tag] = len(aggs)
			aggs = append(aggs, GroupByAgg{Op: OpSum, Field: strings.TrimPrefix(m.tag, "sum:")})
		case strings.HasPrefix(m.tag, "min:"):
			indices[m.tag] = len(aggs)
			aggs = append(aggs, GroupByAgg{Op: OpMin, Field: strings.TrimPrefix(m.tag, "min:")})
		case strings.HasPrefix(m.tag, "max:"):
			indices[m.tag] = len(aggs)
			aggs = append(aggs, GroupByAgg{Op: OpMax, Field: strings.TrimPrefix(m.tag, "max:")})
		}
	}

	return aggs, indices
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
