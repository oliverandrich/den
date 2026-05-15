package den

import (
	"fmt"
	"reflect"

	json "github.com/goccy/go-json"

	"github.com/oliverandrich/den/document"
)

// documentBaseType is the cached reflect.Type of document.Base, used by
// NewLink's structural ID extraction.
var documentBaseType = reflect.TypeFor[document.Base]()

// Link represents a reference to a document in another collection.
// Only the ID is persisted; Value is populated on fetch.
type Link[T any] struct {
	ID     string
	Value  *T
	Loaded bool
}

// MarshalJSON serializes the link as a JSON string (the ID).
func (l Link[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.ID)
}

// UnmarshalJSON deserializes a JSON string into the link.
func (l *Link[T]) UnmarshalJSON(data []byte) error {
	var id string
	if err := json.Unmarshal(data, &id); err != nil {
		return err
	}
	l.ID = id
	l.Value = nil
	l.Loaded = false
	return nil
}

// NewLink creates a Link from an existing document, extracting its ID
// from the embedded document.Base.
//
// The doc must contain a document.Base anywhere in its struct tree —
// directly embedded (the standard pattern), embedded via a wrapper, or
// even as a named field. NewLink panics if no document.Base is found,
// because a Link without an ID is silently broken downstream and
// always indicates a programmer error.
//
// An empty Base.ID (i.e. the doc has not been inserted yet) is fine and
// expected on the LinkWrite cascade path — the cascaded Insert will
// populate the ID and propagate it back into the parent's Link.
func NewLink[T any](doc *T) Link[T] {
	v := reflect.ValueOf(doc).Elem()
	id, ok := extractBaseID(v)
	if !ok {
		panic(fmt.Sprintf("den: NewLink: type %T does not embed document.Base", *doc))
	}
	return Link[T]{ID: id, Value: doc, Loaded: true}
}

// extractBaseID walks v's anonymous-embed chain and returns the ID of the
// first document.Base it finds. Recursion follows the same rule as
// internal.AnalyzeStruct's collectFields — only anonymous struct fields
// are descended — so this function and the StructInfo.BaseID lookup
// always agree on what counts as an ID-bearing Base. Returns ("", false)
// when no document.Base is reachable through anonymous embeds.
//
// Used by NewLink, which has no registered StructInfo available at the
// call site, to obtain the ID without going through the collection
// registry.
func extractBaseID(v reflect.Value) (string, bool) {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "", false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", false
	}
	if v.Type() == documentBaseType {
		return v.FieldByName("ID").String(), true
	}
	t := v.Type()
	for i := range v.NumField() {
		if !t.Field(i).Anonymous {
			continue
		}
		if id, ok := extractBaseID(v.Field(i)); ok {
			return id, true
		}
	}
	return "", false
}

// IsLoaded reports whether the linked document has been fetched.
func (l Link[T]) IsLoaded() bool {
	return l.Loaded
}
