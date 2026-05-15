package den

import (
	"fmt"
	"reflect"
)

// SetFields is a map of field names (as they appear in the `json` struct
// tag) to new values for partial updates via QuerySet.UpdateOne,
// QuerySet.UpsertOne, and QuerySet.Update.
//
// Names are validated against the registered struct before the write
// transaction opens; an unknown name aborts the call without touching
// storage. Callers that want to validate names at application start can
// iterate Meta[T].Fields and compare against a known set.
type SetFields map[string]any

// applySetFields applies a SetFields map to a struct value, validating that
// each named field exists on the collection's struct.
func applySetFields(rv reflect.Value, col *collectionInfo, fields SetFields) error {
	for fieldName, newVal := range fields {
		fi := col.structInfo.FieldByName(fieldName)
		if fi == nil {
			return fmt.Errorf("den: field %q not found in %s", fieldName, col.meta.Name)
		}
		fv := rv.FieldByIndex(fi.Index)
		if err := setFieldValue(fv, newVal, fieldName); err != nil {
			return err
		}
	}
	return nil
}

// validateSetFields checks that every field name in fields exists on the
// collection's struct. Shared by callers that need pre-transaction validation
// (QuerySet.Update) — the in-tx applySetFields re-validates as it goes, so
// within the tx this step is not required.
func validateSetFields(col *collectionInfo, fields SetFields) error {
	for fieldName := range fields {
		if col.structInfo.FieldByName(fieldName) == nil {
			return fmt.Errorf("den: field %q not found in %s", fieldName, col.meta.Name)
		}
	}
	return nil
}

// setFieldValue sets a struct field to the given value, handling nil correctly.
func setFieldValue(fv reflect.Value, newVal any, fieldName string) error {
	if newVal == nil {
		fv.Set(reflect.Zero(fv.Type()))
		return nil
	}
	newRV := reflect.ValueOf(newVal)
	if newRV.Type() == fv.Type() {
		fv.Set(newRV)
		return nil
	}
	if !newRV.Type().ConvertibleTo(fv.Type()) {
		return fmt.Errorf("den: field %q: cannot assign %T to %s", fieldName, newVal, fv.Type())
	}
	fv.Set(newRV.Convert(fv.Type()))
	return nil
}
