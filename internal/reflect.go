package internal

import (
	"reflect"
	"strings"
	"time"
)

// TagOptions holds the parsed options from a den struct tag.
type TagOptions struct {
	Index  bool
	Unique bool
	FTS    bool
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
func ParseDenTag(tag string) TagOptions {
	opts := TagOptions{}
	if tag == "" {
		return opts
	}
	for part := range strings.SplitSeq(tag, ",") {
		switch strings.TrimSpace(part) {
		case "index":
			opts.Index = true
		case "unique":
			opts.Unique = true
		case "fts":
			opts.FTS = true
		}
	}
	return opts
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

	collectFields(t, nil, info)

	// Build index for O(1) FieldByName lookups
	for i, f := range info.Fields {
		info.fieldIndex[f.JSONName] = i
	}

	return info, nil
}

var timePtrType = reflect.TypeFor[*time.Time]()

func collectFields(t reflect.Type, indexPrefix []int, info *StructInfo) {
	for i := range t.NumField() {
		field := t.Field(i)
		index := append(append([]int(nil), indexPrefix...), i)

		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			collectFields(field.Type, index, info)
			continue
		}

		// Field name from json tag, metadata from den tag
		jsonTag := field.Tag.Get("json")
		name := ParseJSONTagName(jsonTag)
		if name == "" {
			name = strings.ToLower(field.Name)
		}

		denTag := field.Tag.Get("den")
		opts := ParseDenTag(denTag)

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
}
