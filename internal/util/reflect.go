package util

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// TagOptions holds the parsed options from a den struct tag.
type TagOptions struct {
	Index          bool
	Unique         bool
	FTS            bool
	OmitEmpty      bool
	Eager          bool   // valid on Link[T] / []Link[T] fields — auto-hydrate by default
	UniqueTogether string // group name for composite unique index
	IndexTogether  string // group name for composite non-unique index
}

// FieldInfo describes a single field in a document struct.
type FieldInfo struct {
	JSONName  string
	GoName    string
	Type      reflect.Type
	Index     []int // reflect index path for nested access
	Options   TagOptions
	IsPointer bool
}

// StructInfo holds analyzed metadata for a document struct.
type StructInfo struct {
	CollectionName string
	Fields         []FieldInfo
	fieldIndex     map[string]int // jsonName → index into Fields
	GoType         reflect.Type
	HasDeletedAt   bool

	// HasValidateTags is true when at least one field — including fields of
	// anonymous embedded structs — carries a non-empty `validate:` struct
	// tag. The write path uses this to skip the go-playground reflective
	// walk for types that have nothing to validate (the dominant case in
	// profiled workloads). Custom Validator.Validate(ctx) hooks are
	// unaffected; only the tag-driven walk is gated.
	HasValidateTags bool

	// Pre-resolved pointers to the base fields embedded by document.Base /
	// document.SoftDelete / document.Tracked. Populated once by
	// AnalyzeStruct so hot paths can skip per-op FieldByName lookups. Any
	// of these may be nil if the struct does not embed the corresponding
	// base field.
	BaseID        *FieldInfo
	BaseRev       *FieldInfo
	BaseCreatedAt *FieldInfo
	BaseUpdatedAt *FieldInfo
	BaseDeletedAt *FieldInfo
}

// FieldByName returns the FieldInfo for the given JSON field name, or nil.
func (s *StructInfo) FieldByName(jsonName string) *FieldInfo {
	if i, ok := s.fieldIndex[jsonName]; ok {
		return &s.Fields[i]
	}
	return nil
}

// IndexedFields returns all fields with the index option set.
func (s *StructInfo) IndexedFields() []FieldInfo {
	var result []FieldInfo
	for _, f := range s.Fields {
		if f.Options.Index {
			result = append(result, f)
		}
	}
	return result
}

// UniqueFields returns all fields with the unique option set.
func (s *StructInfo) UniqueFields() []FieldInfo {
	var result []FieldInfo
	for _, f := range s.Fields {
		if f.Options.Unique {
			result = append(result, f)
		}
	}
	return result
}

// ParseDenTag parses a den struct tag for metadata options only.
// Format: "option1,option2,..." (no field name — that comes from json tag).
// Returns an error for unknown tag options to catch typos like "indx".
func ParseDenTag(tag string) (TagOptions, error) {
	opts := TagOptions{}
	if tag == "" {
		return opts, nil
	}
	for part := range strings.SplitSeq(tag, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		switch {
		case trimmed == "index":
			opts.Index = true
		case trimmed == "unique":
			opts.Unique = true
		case trimmed == "fts":
			opts.FTS = true
		case trimmed == "omitempty":
			opts.OmitEmpty = true
		case trimmed == "eager":
			opts.Eager = true
		case strings.HasPrefix(trimmed, "unique_together:"):
			opts.UniqueTogether = strings.TrimPrefix(trimmed, "unique_together:")
		case strings.HasPrefix(trimmed, "index_together:"):
			opts.IndexTogether = strings.TrimPrefix(trimmed, "index_together:")
		default:
			return opts, fmt.Errorf("unknown den tag option %q", trimmed)
		}
	}
	return opts, nil
}

// ParseJSONTagName extracts the field name from a json struct tag.
// Returns "" if no json tag or if tagged with "-".
func ParseJSONTagName(tag string) string {
	if tag == "" || tag == "-" {
		return ""
	}
	name, _, _ := strings.Cut(tag, ",")
	return name
}

// CollectionName derives the collection name from a Go type name.
// Simply lowercases the full name, no pluralization.
func CollectionName(typeName string) string {
	return strings.ToLower(typeName)
}

// AnalyzeStruct analyzes a struct type and extracts field metadata
// from json and den struct tags. Embedded structs are flattened.
func AnalyzeStruct(t reflect.Type) (*StructInfo, error) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	info := &StructInfo{
		CollectionName: CollectionName(t.Name()),
		GoType:         t,
		fieldIndex:     make(map[string]int),
	}

	if err := collectFields(t, nil, info); err != nil {
		return nil, err
	}

	info.HasValidateTags = scanValidateTags(t, make(map[reflect.Type]bool))

	// Build index for O(1) FieldByName lookups
	for i, f := range info.Fields {
		info.fieldIndex[f.JSONName] = i
	}

	// Pre-resolve base-field pointers once. These references are stable
	// because info.Fields is not mutated after this point.
	info.BaseID = info.FieldByName("_id")
	info.BaseRev = info.FieldByName("_rev")
	info.BaseCreatedAt = info.FieldByName("_created_at")
	info.BaseUpdatedAt = info.FieldByName("_updated_at")
	info.BaseDeletedAt = info.FieldByName("_deleted_at")

	return info, nil
}

var (
	timePtrType = reflect.TypeFor[*time.Time]()
	timeType    = reflect.TypeFor[time.Time]()
)

// scanValidateTags reports whether any field reachable from t — through
// anonymous embeds, named struct fields, pointers, slices, arrays, and
// map values — carries a non-empty `validate:` struct tag. Mirrors the
// type-tree traversal go-playground/validator performs at runtime: if
// scanValidateTags returns false, validator.Struct(t) has no work to do
// and the write path can skip the call entirely.
//
// seen breaks recursion on self-referential types (e.g. a struct that
// holds a pointer to its own kind).
func scanValidateTags(t reflect.Type, seen map[reflect.Type]bool) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if seen[t] {
		return false
	}
	seen[t] = true

	switch t.Kind() { //nolint:exhaustive // scalars and unsupported kinds cannot carry validate tags
	case reflect.Struct:
		// time.Time is the one stdlib value type Den documents commonly
		// hold by value. Skip it explicitly so the walk does not descend
		// into stdlib internals looking for tags that cannot exist.
		if t == timeType {
			return false
		}
		for i := range t.NumField() {
			f := t.Field(i)
			tag := f.Tag.Get("validate")
			// go-playground/validator treats `validate:"-"` as an explicit
			// skip that also blocks descent into the field's type. Mirror
			// that so a type whose only tags are `"-"` short-circuits like
			// a tagless type and does not pay the walker cost.
			if tag == "-" {
				continue
			}
			if tag != "" {
				return true
			}
			if scanValidateTags(f.Type, seen) {
				return true
			}
		}
	case reflect.Slice, reflect.Array, reflect.Map:
		return scanValidateTags(t.Elem(), seen)
	}
	return false
}

func collectFields(t reflect.Type, indexPrefix []int, info *StructInfo) error {
	for i := range t.NumField() {
		field := t.Field(i)
		index := append(append([]int(nil), indexPrefix...), i)

		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			if err := collectFields(field.Type, index, info); err != nil {
				return err
			}
			continue
		}

		// Field name from json tag, metadata from den tag
		jsonTag := field.Tag.Get("json")
		name := ParseJSONTagName(jsonTag)
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		if err := ValidateFieldName(name); err != nil {
			return fmt.Errorf("field %s: %w", field.Name, err)
		}

		denTag := field.Tag.Get("den")
		opts, err := ParseDenTag(denTag)
		if err != nil {
			return fmt.Errorf("field %s: %w", field.Name, err)
		}

		isPointer := field.Type.Kind() == reflect.Pointer

		fi := FieldInfo{
			JSONName:  name,
			GoName:    field.Name,
			Type:      field.Type,
			Index:     index,
			Options:   opts,
			IsPointer: isPointer,
		}

		info.Fields = append(info.Fields, fi)

		if name == "_deleted_at" && (field.Type == timePtrType) {
			info.HasDeletedAt = true
		}
	}
	return nil
}
