package den

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
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
//
// Scalar aggregates ignore Limit, Skip, Sort, After, and Before — they
// always operate on the full WHERE-filtered set.
func (qs QuerySet[T]) Avg(ctx context.Context, field string) (float64, error) {
	return qs.aggregate(ctx, OpAvg, field)
}

// Sum returns the sum of the given field across matching documents.
// See Avg for the modifier-applicability rules.
func (qs QuerySet[T]) Sum(ctx context.Context, field string) (float64, error) {
	return qs.aggregate(ctx, OpSum, field)
}

// Min returns the minimum value of the given field across matching documents.
// See Avg for the modifier-applicability rules.
func (qs QuerySet[T]) Min(ctx context.Context, field string) (float64, error) {
	return qs.aggregate(ctx, OpMin, field)
}

// Max returns the maximum value of the given field across matching documents.
// See Avg for the modifier-applicability rules.
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
		var docMap map[string]any
		if err := enc.Decode(iter.Bytes(), &docMap); err != nil {
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

// GroupByBuilder allows specifying group-by fields. The builder is typically
// obtained from QuerySet.GroupBy.
type GroupByBuilder[T any] struct {
	qs     QuerySet[T]
	fields []string
	sort   []GroupBySortEntry
}

// OrderByAgg appends an ORDER BY entry that sorts grouped results by an
// aggregate expression. Op selects the aggregate column; field names its
// source field (ignored for OpCount, which sorts by COUNT(*)). Multiple
// calls define tie-breakers in the order they were added.
//
// To order by a group key, use the ordinary QuerySet.Sort chain on the
// underlying query set — Sort fields that match a group key translate to
// ORDER BY the group-key expression. Sort fields that are neither a group
// key nor an aggregate error out at Into.
func (gb GroupByBuilder[T]) OrderByAgg(op AggregateOp, field string, dir SortDirection) GroupByBuilder[T] {
	gb.sort = append(slices.Clone(gb.sort), GroupBySortEntry{Op: op, Field: field, Dir: dir})
	return gb
}

// GroupBy starts a group-by aggregation on one or more fields.
//
// The target struct passed to Into must carry one field tagged `den:"group_key:N"`
// for each field listed here, with N running 0..len(fields)-1. The legacy
// unindexed `den:"group_key"` is accepted when exactly one field is requested
// and is treated as slot 0; mixing the unindexed form with positional tags
// returns an error.
func (qs QuerySet[T]) GroupBy(fields ...string) GroupByBuilder[T] {
	return GroupByBuilder[T]{qs: qs, fields: fields}
}

// Into executes the group-by aggregation and maps results into the target slice.
// The query is pushed down to the database as a SQL GROUP BY statement.
func (gb GroupByBuilder[T]) Into(ctx context.Context, target any) error {
	if err := gb.qs.preflight(); err != nil {
		return err
	}
	if len(gb.fields) == 0 {
		return fmt.Errorf("den: GroupBy requires at least one field")
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

	keySlots, err := resolveGroupKeySlots(mappings, len(gb.fields))
	if err != nil {
		return err
	}

	// Build the list of aggregate expressions from the target struct's den tags.
	aggs, aggIndices, err := buildAggsFromMappings(mappings)
	if err != nil {
		return err
	}

	q := gb.qs.buildBackendQuery(col)

	// Sort fields on the parent QuerySet must reference a group key — they
	// translate to ORDER BY the group-key expression. Aggregate ordering
	// must go through GroupByBuilder.OrderByAgg.
	groupKeySet := make(map[string]struct{}, len(gb.fields))
	for _, f := range gb.fields {
		groupKeySet[f] = struct{}{}
	}
	for _, sf := range q.SortFields {
		if _, ok := groupKeySet[sf.Field]; !ok {
			return fmt.Errorf("den: GroupBy: Sort field %q is not a group key; use OrderByAgg for aggregate sort", sf.Field)
		}
	}
	q.GroupBySort = gb.sort

	rows, err := gb.qs.scope.readWriter().GroupBy(ctx, col.meta.Name, gb.fields, aggs, q)
	if err != nil {
		return err
	}

	for _, row := range rows {
		elem := reflect.New(elemType).Elem()

		// Populate group-key fields by slot.
		for slot, structIdx := range keySlots {
			if structIdx < 0 {
				continue
			}
			fv := elem.Field(structIdx)
			targetType := elemType.Field(structIdx).Type
			var keyVal string
			if slot < len(row.Keys) {
				keyVal = row.Keys[slot]
			}
			kv := reflect.ValueOf(keyVal)
			if kv.Type().ConvertibleTo(targetType) {
				fv.Set(kv.Convert(targetType))
			}
		}

		// Populate aggregate fields by tag.
		for _, m := range mappings {
			if isGroupKeyTag(m.tag) {
				continue
			}
			idx, ok := aggIndices[m.tag]
			if !ok {
				continue
			}
			fv := elem.Field(m.index)
			if m.tag == "count" {
				fv.SetInt(int64(row.Values[idx]))
			} else {
				fv.SetFloat(row.Values[idx])
			}
		}

		sliceVal = reflect.Append(sliceVal, elem)
	}

	rv.Elem().Set(sliceVal)
	return nil
}

// isGroupKeyTag reports whether tag is either the legacy "group_key" form or
// the positional "group_key:N" form.
func isGroupKeyTag(tag string) bool {
	return tag == "group_key" || strings.HasPrefix(tag, "group_key:")
}

// resolveGroupKeySlots returns a slice of length numFields mapping slot →
// struct-field index. It validates that:
//   - each slot 0..numFields-1 is claimed by exactly one struct field,
//   - the legacy unindexed "group_key" tag is only used when numFields == 1,
//   - unindexed and positional tag forms are not mixed.
//
// Returns -1 in a slot only when a field genuinely does not participate (not
// permitted today — every slot must be claimed; missing slots produce an
// error).
func resolveGroupKeySlots(mappings []groupField, numFields int) ([]int, error) {
	slots := make([]int, numFields)
	for i := range slots {
		slots[i] = -1
	}

	var hasUnindexed, hasPositional bool
	for _, m := range mappings {
		switch {
		case m.tag == "group_key":
			hasUnindexed = true
			if numFields != 1 {
				return nil, fmt.Errorf("den: tag `group_key` without slot requires exactly one GroupBy field; have %d", numFields)
			}
			if slots[0] != -1 {
				return nil, fmt.Errorf("den: multiple struct fields claim group_key slot 0")
			}
			slots[0] = m.index
		case strings.HasPrefix(m.tag, "group_key:"):
			hasPositional = true
			slotStr := strings.TrimPrefix(m.tag, "group_key:")
			slot, err := strconv.Atoi(slotStr)
			if err != nil {
				return nil, fmt.Errorf("den: invalid group_key slot %q: %w", slotStr, err)
			}
			if slot < 0 || slot >= numFields {
				return nil, fmt.Errorf("den: group_key:%d out of range [0..%d)", slot, numFields)
			}
			if slots[slot] != -1 {
				return nil, fmt.Errorf("den: multiple struct fields claim group_key slot %d", slot)
			}
			slots[slot] = m.index
		}
	}

	if hasUnindexed && hasPositional {
		return nil, fmt.Errorf("den: cannot mix `group_key` with `group_key:N` tags on the same target")
	}

	for i, idx := range slots {
		if idx == -1 {
			return nil, fmt.Errorf("den: missing struct field for group_key slot %d", i)
		}
	}
	return slots, nil
}

// buildAggsFromMappings converts den struct tags into GroupByAgg descriptors.
// Returns the aggs and a map from tag → index in the Values slice.
//
// Duplicate aggregate tags (two struct fields carrying the same `den:"sum:x"`
// or similar) are rejected with an error. The previous behaviour silently
// overwrote the indices entry, leaving the SQL with a redundant column and
// masking copy-paste typos like "I meant sum:x but typed avg:x twice."
// Mirrors the duplicate-slot rejection that already protects `group_key:N`.
func buildAggsFromMappings(mappings []groupField) ([]GroupByAgg, map[string]int, error) {
	var aggs []GroupByAgg
	indices := make(map[string]int)

	register := func(m groupField, op AggregateOp, field string) error {
		if _, exists := indices[m.tag]; exists {
			return fmt.Errorf("den: GroupBy: duplicate aggregate tag %q on target struct", m.tag)
		}
		indices[m.tag] = len(aggs)
		aggs = append(aggs, GroupByAgg{Op: op, Field: field})
		return nil
	}

	for _, m := range mappings {
		var err error
		switch {
		case isGroupKeyTag(m.tag):
			continue
		case m.tag == "count":
			err = register(m, OpCount, "")
		case strings.HasPrefix(m.tag, "avg:"):
			err = register(m, OpAvg, strings.TrimPrefix(m.tag, "avg:"))
		case strings.HasPrefix(m.tag, "sum:"):
			err = register(m, OpSum, strings.TrimPrefix(m.tag, "sum:"))
		case strings.HasPrefix(m.tag, "min:"):
			err = register(m, OpMin, strings.TrimPrefix(m.tag, "min:"))
		case strings.HasPrefix(m.tag, "max:"):
			err = register(m, OpMax, strings.TrimPrefix(m.tag, "max:"))
		}
		if err != nil {
			return nil, nil, err
		}
	}

	return aggs, indices, nil
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
