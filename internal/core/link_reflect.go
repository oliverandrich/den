package core

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/oliverandrich/den/internal/util"
)

// linkFieldInfo describes a single Link or []Link field in a struct,
// plus the pre-resolved index paths for the Link[T] sub-fields so hot
// paths can use FieldByIndex instead of per-op FieldByName lookups.
type linkFieldInfo struct {
	index      int          // field index in the parent struct
	slice      bool         // true for []Link[T], false for Link[T]
	idIdx      []int        // index path for Link[T].ID
	valueIdx   []int        // index path for Link[T].Value
	loadedIdx  []int        // index path for Link[T].Loaded
	targetType reflect.Type // T in Link[T] (derived from the Value *T field)
	eager      bool         // true when the field is tagged den:"eager"
}

// skipForMode reports whether a hydration mode should leave this field
// untouched. fetchDefault hydrates only eager-tagged fields; the other
// modes are decided one level up and never reach this predicate.
func (lf linkFieldInfo) skipForMode(mode fetchMode) bool {
	return mode == fetchDefault && !lf.eager
}

// linkFieldsBundle is the cached per-type analysis used by both the
// batched and per-row hydration paths. anyEager lets terminals skip
// hydration work entirely on types that have no eager-tagged links —
// no reflect walk on the hot path.
type linkFieldsBundle struct {
	fields   []linkFieldInfo
	anyEager bool
}

var linkFieldCache sync.Map // reflect.Type → linkFieldsBundle

func loadLinkFieldsBundle(t reflect.Type) linkFieldsBundle {
	if cached, ok := linkFieldCache.Load(t); ok {
		bundle, _ := cached.(linkFieldsBundle)
		return bundle
	}

	var fields []linkFieldInfo
	anyEager := false
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous {
			continue
		}
		ft := f.Type
		var linkType reflect.Type
		slice := false
		switch {
		case util.IsLinkShape(ft):
			linkType = ft
		case ft.Kind() == reflect.Slice && util.IsLinkShape(ft.Elem()):
			linkType = ft.Elem()
			slice = true
		}
		if linkType == nil {
			continue
		}
		idF, _ := linkType.FieldByName("ID")
		valF, _ := linkType.FieldByName("Value")
		loadF, _ := linkType.FieldByName("Loaded")
		// Best-effort tag parse: an unknown den-tag option here would be
		// rejected at Register time anyway, so a parse error is silently
		// dropped (the eager flag stays false).
		opts, _ := util.ParseDenTag(f.Tag.Get("den"))
		if opts.Eager {
			anyEager = true
		}
		fields = append(fields, linkFieldInfo{
			index:      i,
			slice:      slice,
			idIdx:      idF.Index,
			valueIdx:   valF.Index,
			loadedIdx:  loadF.Index,
			targetType: valF.Type.Elem(), // Value is *T, Elem() is T
			eager:      opts.Eager,
		})
	}

	bundle := linkFieldsBundle{fields: fields, anyEager: anyEager}
	linkFieldCache.Store(t, bundle)
	return bundle
}

// getLinkFields returns the cached list of link field indices for a struct type.
func getLinkFields(t reflect.Type) []linkFieldInfo {
	return loadLinkFieldsBundle(t).fields
}

// hasEagerLinkFields reports whether T has at least one Link[T] or
// []Link[T] field tagged den:"eager". Used by terminals to skip
// hydration work in the default fetch mode without walking the slice.
func hasEagerLinkFields(t reflect.Type) bool {
	return loadLinkFieldsBundle(t).anyEager
}

// validateEagerTags rejects den:"eager" placed on a field that is not
// Link[T] or []Link[T]. Other tag/type mismatches (unique on a non-string,
// fts on a non-string) are caught at Register time the same way; eager
// follows the pattern so a misplaced tag fails loud at startup instead
// of being silently ignored.
func validateEagerTags(info *util.StructInfo) error {
	for _, f := range info.Fields {
		if !f.Options.Eager {
			continue
		}
		ft := f.Type
		isLink := util.IsLinkShape(ft)
		isSliceOfLink := ft.Kind() == reflect.Slice && util.IsLinkShape(ft.Elem())
		if !isLink && !isSliceOfLink {
			return fmt.Errorf(
				`den: tag den:"eager" on field %q (%s): only valid on Link[T] or []Link[T]`,
				f.GoName, ft.String(),
			)
		}
	}
	return nil
}

// forEachLinkField iterates over Link and []Link fields of a struct,
// calling fn for each individual link element together with its pre-
// resolved linkFieldInfo so callbacks can use FieldByIndex instead of
// FieldByName.
//
// Cancelling ctx stops the walk between link elements; the inner backend
// calls in fn already honor ctx, so the explicit check upper-bounds the
// latency to one link field rather than one more backend round-trip.
func forEachLinkField(ctx context.Context, doc any, fn func(elem reflect.Value, lf linkFieldInfo) error) error {
	t := reflect.TypeOf(doc).Elem()
	bundle := loadLinkFieldsBundle(t)
	if len(bundle.fields) == 0 {
		return nil
	}

	v := reflect.ValueOf(doc).Elem()
	for _, lf := range bundle.fields {
		if err := ctx.Err(); err != nil {
			return err
		}
		fv := v.Field(lf.index)
		if lf.slice {
			for j := range fv.Len() {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := fn(fv.Index(j), lf); err != nil {
					return err
				}
			}
		} else {
			if err := fn(fv, lf); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseJSONTagName delegates to util.ParseJSONTagName.
var parseJSONTagName = util.ParseJSONTagName
