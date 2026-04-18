package den

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	json "github.com/goccy/go-json"

	"github.com/oliverandrich/den/internal"
)

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

// NewLink creates a Link from an existing document, extracting its ID.
func NewLink[T any](doc *T) Link[T] {
	v := reflect.ValueOf(doc).Elem()

	// Look for an ID field
	idField := v.FieldByName("ID")
	var id string
	if idField.IsValid() && idField.Kind() == reflect.String {
		id = idField.String()
	}

	return Link[T]{ID: id, Value: doc, Loaded: true}
}

// IsLoaded reports whether the linked document has been fetched.
func (l Link[T]) IsLoaded() bool {
	return l.Loaded
}

// LinkRule controls cascading behavior for write and delete operations.
type LinkRule int

const (
	LinkIgnore LinkRule = iota
	LinkWrite
	LinkDelete
)

// CRUDOption configures CRUD operations.
type CRUDOption func(*crudOpts)

type crudOpts struct {
	linkRule       LinkRule
	ignoreRevision bool
	hardDelete     bool
}

// WithLinkRule sets the link cascading rule for Insert/Update/Delete.
func WithLinkRule(rule LinkRule) CRUDOption {
	return func(o *crudOpts) {
		o.linkRule = rule
	}
}

// FetchLink resolves a single named link field on a document.
func FetchLink[T any](ctx context.Context, db *DB, doc *T, fieldName string) error {
	return fetchLinkByName(ctx, db, db.backend, doc, fieldName, 1)
}

// FetchAllLinks resolves all link fields on a document.
func FetchAllLinks[T any](ctx context.Context, db *DB, doc *T) error {
	return fetchAllLinksOnDoc(ctx, db, db.backend, doc, 1)
}

// fetchLinkByName resolves one named link field. The rw parameter carries
// the ReadWriter that should service the actual Get for the linked document:
// pass a Transaction from inside an open iterator so the read reuses the
// same connection (avoids pool exhaustion) and, on stricter isolation
// levels, the same snapshot. Fall back to db.backend when no tx is open.
func fetchLinkByName(ctx context.Context, db *DB, rw ReadWriter, doc any, fieldName string, depth int) error {
	if depth <= 0 {
		return nil
	}

	v := reflect.ValueOf(doc).Elem()
	t := v.Type()

	for _, lf := range getLinkFields(t) {
		field := t.Field(lf.index)
		name := parseJSONTagName(field.Tag.Get("json"))
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		if name != fieldName {
			continue
		}

		fv := v.Field(lf.index)
		if lf.slice {
			for j := range fv.Len() {
				if err := resolveSingleLink(ctx, db, rw, fv.Index(j), lf); err != nil {
					return err
				}
			}
			return nil
		}
		return resolveSingleLink(ctx, db, rw, fv, lf)
	}
	return fmt.Errorf("den: link field %q not found", fieldName)
}

func fetchAllLinksOnDoc(ctx context.Context, db *DB, rw ReadWriter, doc any, depth int) error {
	if depth <= 0 {
		return nil
	}

	return forEachLinkField(doc, func(elem reflect.Value, lf linkFieldInfo) error {
		return resolveSingleLink(ctx, db, rw, elem, lf)
	})
}

func resolveSingleLink(ctx context.Context, db *DB, rw ReadWriter, linkVal reflect.Value, lf linkFieldInfo) error {
	idField := linkVal.FieldByIndex(lf.idIdx)
	if idField.String() == "" {
		return nil
	}

	loadedField := linkVal.FieldByIndex(lf.loadedIdx)
	if loadedField.Bool() {
		return nil // already loaded
	}

	id := idField.String()
	valueField := linkVal.FieldByIndex(lf.valueIdx)

	// Determine the target type (the T in Link[T])
	targetType := valueField.Type().Elem() // *T → T

	// Look up the collection for this type (respects custom CollectionName)
	col, err := collectionForType(db, targetType)
	if err != nil {
		return err
	}
	colName := col.meta.Name

	// Fetch the document via the caller-supplied ReadWriter so that,
	// when called from inside an iterator's TX, the read reuses the same
	// connection instead of grabbing a second one from the pool.
	data, err := rw.Get(ctx, colName, id)
	if err != nil {
		return err
	}

	// Decode into a new instance of T
	target := reflect.New(targetType)
	if err := db.decode(data, target.Interface()); err != nil {
		return fmt.Errorf("decode linked %s: %w", colName, err)
	}
	captureSnapshot(data, target.Interface())

	valueField.Set(target)
	loadedField.SetBool(true)
	return nil
}

// linkFieldInfo describes a single Link or []Link field in a struct,
// plus the pre-resolved index paths for the Link[T] sub-fields so hot
// paths can use FieldByIndex instead of per-op FieldByName lookups.
type linkFieldInfo struct {
	index     int   // field index in the parent struct
	slice     bool  // true for []Link[T], false for Link[T]
	idIdx     []int // index path for Link[T].ID
	valueIdx  []int // index path for Link[T].Value
	loadedIdx []int // index path for Link[T].Loaded
}

var linkFieldCache sync.Map // reflect.Type → []linkFieldInfo

// getLinkFields returns the cached list of link field indices for a struct type.
func getLinkFields(t reflect.Type) []linkFieldInfo {
	if cached, ok := linkFieldCache.Load(t); ok {
		v, _ := cached.([]linkFieldInfo)
		return v
	}

	var fields []linkFieldInfo
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous {
			continue
		}
		ft := f.Type
		var linkType reflect.Type
		slice := false
		switch {
		case ft.Kind() == reflect.Struct && detectLinkType(ft):
			linkType = ft
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct && detectLinkType(ft.Elem()):
			linkType = ft.Elem()
			slice = true
		}
		if linkType == nil {
			continue
		}
		idF, _ := linkType.FieldByName("ID")
		valF, _ := linkType.FieldByName("Value")
		loadF, _ := linkType.FieldByName("Loaded")
		fields = append(fields, linkFieldInfo{
			index:     i,
			slice:     slice,
			idIdx:     idF.Index,
			valueIdx:  valF.Index,
			loadedIdx: loadF.Index,
		})
	}

	linkFieldCache.Store(t, fields)
	return fields
}

func detectLinkType(t reflect.Type) bool {
	if t.NumField() < 3 {
		return false
	}
	idField, hasID := t.FieldByName("ID")
	_, hasValue := t.FieldByName("Value")
	_, hasLoaded := t.FieldByName("Loaded")
	return hasID && hasValue && hasLoaded && idField.Type.Kind() == reflect.String
}

// forEachLinkField iterates over Link and []Link fields of a struct,
// calling fn for each individual link element together with its pre-
// resolved linkFieldInfo so callbacks can use FieldByIndex instead of
// FieldByName.
func forEachLinkField(doc any, fn func(elem reflect.Value, lf linkFieldInfo) error) error {
	v := reflect.ValueOf(doc).Elem()

	for _, lf := range getLinkFields(v.Type()) {
		fv := v.Field(lf.index)
		if lf.slice {
			for j := range fv.Len() {
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

func applyCRUDOpts(opts []CRUDOption) crudOpts {
	var o crudOpts
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// cascadeWriteLinks saves all linked documents that have a Value set.
func cascadeWriteLinks(ctx context.Context, db *DB, b ReadWriter, doc any) error {
	return forEachLinkField(doc, func(elem reflect.Value, lf linkFieldInfo) error {
		return saveSingleLinkedValue(ctx, db, b, elem, lf)
	})
}

// cascadeDeleteLinks deletes all linked documents.
func cascadeDeleteLinks(ctx context.Context, db *DB, b ReadWriter, doc any) error {
	return forEachLinkField(doc, func(elem reflect.Value, lf linkFieldInfo) error {
		return deleteSingleLinkedValue(ctx, db, b, elem, lf)
	})
}

func saveSingleLinkedValue(ctx context.Context, db *DB, b ReadWriter, linkVal reflect.Value, lf linkFieldInfo) error {
	valueField := linkVal.FieldByIndex(lf.valueIdx)
	if valueField.IsNil() {
		return nil
	}

	target := valueField.Interface()
	targetType := valueField.Type().Elem()

	col, err := collectionForType(db, targetType)
	if err != nil {
		return err
	}

	tv := reflect.ValueOf(target).Elem()

	id := getID(tv, col.structInfo)
	isInsert := id == ""

	if isInsert {
		if err := runBeforeInsertHooks(ctx, target); err != nil {
			return err
		}
	} else {
		if err := runBeforeUpdateHooks(ctx, target); err != nil {
			return err
		}
	}

	if db.tagValidator != nil {
		if err := db.tagValidator(target); err != nil {
			return fmt.Errorf("%w: %w", ErrValidation, err)
		}
	}

	if err := runValidationHooks(ctx, target); err != nil {
		return err
	}

	now := time.Now()
	setBaseFields(tv, col.structInfo, now, isInsert)

	if col.settings.UseRevision {
		if isInsert {
			setRevision(tv, col.structInfo, newRevision())
		} else {
			if err := checkAndUpdateRevision(ctx, db, b, col, tv, false); err != nil {
				return err
			}
		}
	}

	data, err := db.encode(target)
	if err != nil {
		return fmt.Errorf("encode linked %s: %w", col.meta.Name, err)
	}

	id = getID(tv, col.structInfo)
	linkVal.FieldByIndex(lf.idIdx).SetString(id)

	if err := b.Put(ctx, col.meta.Name, id, data); err != nil {
		return err
	}
	captureSnapshot(data, target)

	if isInsert {
		return runAfterInsertHooks(ctx, target)
	}
	return runAfterUpdateHooks(ctx, target)
}

func deleteSingleLinkedValue(ctx context.Context, db *DB, b ReadWriter, linkVal reflect.Value, lf linkFieldInfo) error {
	idField := linkVal.FieldByIndex(lf.idIdx)
	if idField.String() == "" {
		return nil
	}

	valueField := linkVal.FieldByIndex(lf.valueIdx)

	id := idField.String()
	targetType := valueField.Type().Elem()

	col, err := collectionForType(db, targetType)
	if err != nil {
		return err
	}
	colName := col.meta.Name

	// Load the linked document to run hooks and handle soft-delete
	data, err := b.Get(ctx, colName, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil // already deleted
		}
		return err
	}

	docPtr := reflect.New(targetType)
	doc := docPtr.Interface()
	if err := db.decode(data, doc); err != nil {
		return fmt.Errorf("decode linked %s: %w", colName, err)
	}

	if err := runBeforeDeleteHooks(ctx, doc); err != nil {
		return err
	}

	if col.meta.HasSoftBase {
		now := time.Now()
		setSoftDeletedAt(docPtr.Elem(), col.structInfo, &now)

		encoded, err := db.encode(doc)
		if err != nil {
			return fmt.Errorf("encode soft delete linked %s: %w", colName, err)
		}
		if err := b.Put(ctx, colName, id, encoded); err != nil {
			return err
		}
		return runAfterDeleteHooks(ctx, doc)
	}

	if err := b.Delete(ctx, colName, id); err != nil {
		return err
	}
	return runAfterDeleteHooks(ctx, doc)
}

// parseJSONTagName delegates to internal.ParseJSONTagName.
var parseJSONTagName = internal.ParseJSONTagName
