package engine

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/oliverandrich/den/internal/util"
)

// Marshal is Den's JSON marshaller for output: it behaves exactly like
// [encoding/json.Marshal], except that any hydrated (Loaded) Link[T] /
// []Link[T] anywhere in the value graph is emitted as its resolved Value
// object instead of the bare id. Unloaded links, and every non-link field,
// marshal identically to the standard library.
//
// This is additive — the default wire format produced by json.Marshal (and
// therefore Den's storage encoding) is unchanged; only Marshal opts into
// expansion. Hydrate the links you want expanded with QuerySet.WithFetchLinks
// (optionally naming specific fields), then call Marshal for the response:
// the set of loaded links is exactly the set that expands.
//
// Note: objects that actually contain an expanded link are re-encoded from a
// map, so their JSON keys come out in sorted order rather than struct-
// declaration order (JSON objects are unordered). Values with no loaded link
// are returned byte-for-byte as json.Marshal would produce them.
func Marshal(v any) ([]byte, error) {
	return marshalValue(reflect.ValueOf(v))
}

func marshalValue(rv reflect.Value) ([]byte, error) {
	if !rv.IsValid() {
		return []byte("null"), nil
	}
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return []byte("null"), nil
		}
		rv = rv.Elem()
	}
	rt := rv.Type()

	if util.IsLinkShape(rt) {
		if rv.FieldByName("Loaded").Bool() {
			if val := rv.FieldByName("Value"); !val.IsNil() {
				return marshalValue(val) // expand the resolved object
			}
		}
		return json.Marshal(rv.Interface()) // default: bare id
	}

	if !typeContainsLink(rt) {
		return json.Marshal(rv.Interface())
	}

	switch rt.Kind() { //nolint:exhaustive // only composite kinds can contain a link; all others took the fast path above
	case reflect.Struct:
		return marshalStruct(rv)
	case reflect.Slice, reflect.Array:
		return marshalSequence(rv)
	case reflect.Map:
		return marshalMap(rv)
	default:
		return json.Marshal(rv.Interface())
	}
}

// marshalStruct marshals rv normally, then replaces the JSON value of every
// link-bearing field with its expanded form. When nothing actually expands,
// the original bytes are returned untouched (preserving key order and exact
// formatting), so the no-loaded-links case is byte-identical to json.Marshal.
func marshalStruct(rv reflect.Value) ([]byte, error) {
	raw, err := json.Marshal(rv.Interface())
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		// Custom MarshalJSON emitted a non-object — leave it as-is.
		return raw, nil //nolint:nilerr // intentional: respect custom encoding
	}
	changed, err := patchStructLinks(rv, m)
	if err != nil {
		return nil, err
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(m)
}

// patchStructLinks walks rv's fields and, for each that may contain a link,
// overwrites m[jsonKey] with the field's expanded JSON. Anonymous embedded
// structs are flattened into the same map (matching json's promotion).
// Returns whether any value actually differed from what json.Marshal produced.
func patchStructLinks(rv reflect.Value, m map[string]json.RawMessage) (bool, error) {
	rt := rv.Type()
	changed := false
	for i := range rt.NumField() {
		f := rt.Field(i)
		if f.PkgPath != "" { // unexported (incl. unexported embeds)
			continue
		}
		name, skip, named := jsonTag(f)
		if skip {
			continue
		}
		fv := rv.Field(i)

		if f.Anonymous && !named {
			ft, efv := f.Type, fv
			if ft.Kind() == reflect.Pointer {
				if efv.IsNil() {
					continue
				}
				ft, efv = ft.Elem(), efv.Elem()
			}
			if ft.Kind() == reflect.Struct && typeContainsLink(ft) {
				c, err := patchStructLinks(efv, m)
				if err != nil {
					return false, err
				}
				changed = changed || c
			}
			continue
		}

		if !typeContainsLink(f.Type) {
			continue
		}
		raw, ok := m[name]
		if !ok { // omitempty-absent or json:"-"
			continue
		}
		b, err := marshalValue(fv)
		if err != nil {
			return false, err
		}
		if !bytes.Equal(b, raw) {
			m[name] = b
			changed = true
		}
	}
	return changed, nil
}

func marshalSequence(rv reflect.Value) ([]byte, error) {
	if rv.Kind() == reflect.Slice && rv.IsNil() {
		return []byte("null"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i := range rv.Len() {
		if i > 0 {
			buf.WriteByte(',')
		}
		b, err := marshalValue(rv.Index(i))
		if err != nil {
			return nil, err
		}
		buf.Write(b)
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

func marshalMap(rv reflect.Value) ([]byte, error) {
	if rv.IsNil() {
		return []byte("null"), nil
	}
	if rv.Type().Key().Kind() != reflect.String {
		// Non-string keys: rare and link-bearing values here are exotic; fall
		// back to stdlib (no expansion).
		return json.Marshal(rv.Interface())
	}
	keys := rv.MapKeys()
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k.String())
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := marshalValue(rv.MapIndex(k))
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// jsonTag reports the JSON key for a field, whether it is skipped (json:"-"),
// and whether the tag gave it an explicit name. The key falls back to the
// exact Go field name — matching what encoding/json emits for untagged fields.
func jsonTag(f reflect.StructField) (name string, skip, named bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", true, false
	}
	first, _, _ := strings.Cut(tag, ",")
	if first == "" {
		return f.Name, false, false
	}
	return first, false, true
}

var containsLinkCache sync.Map // reflect.Type → bool

// typeContainsLink reports whether t (a struct/slice/map/pointer graph) has a
// Link[T] anywhere within it, so the fast path can hand link-free values
// straight to json.Marshal. Results are cached per type.
func typeContainsLink(t reflect.Type) bool {
	if v, ok := containsLinkCache.Load(t); ok {
		b, _ := v.(bool) // only bools are stored
		return b
	}
	res := computeContainsLink(t, map[reflect.Type]bool{})
	containsLinkCache.Store(t, res)
	return res
}

func computeContainsLink(t reflect.Type, seen map[reflect.Type]bool) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if util.IsLinkShape(t) { // short-circuits before recursing into Value's T
		return true
	}
	if seen[t] {
		return false
	}
	seen[t] = true
	switch t.Kind() { //nolint:exhaustive // scalar kinds cannot hold a link; only composites recurse
	case reflect.Struct:
		for i := range t.NumField() {
			if t.Field(i).PkgPath != "" {
				continue
			}
			if computeContainsLink(t.Field(i).Type, seen) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		return computeContainsLink(t.Elem(), seen)
	case reflect.Map:
		return computeContainsLink(t.Elem(), seen)
	}
	return false
}
