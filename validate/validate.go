// Package validate exposes the struct-tag constraint helper used by Den.
//
// Den runs Document automatically on every Insert and Update — there is
// no opt-in option, and there is no way to bypass tag-level constraints
// from inside Den. Call Document directly only at boundaries before the
// doc reaches Den (typical use: HTTP handlers that want to reject bad
// input before opening a database transaction). The returned *Errors
// mirrors what Den's write path would have produced.
//
// The struct-tag syntax follows go-playground/validator/v10:
//
//	type Product struct {
//	    document.Base
//	    Name  string  `json:"name"  validate:"required,min=3"`
//	    Email string  `json:"email" validate:"required,email"`
//	    Price float64 `json:"price" validate:"required,min=0"`
//	}
package validate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/oliverandrich/den/document"
)

// FieldError describes a single field that failed validation.
type FieldError struct {
	Field string // struct field name
	Tag   string // validation tag that failed (e.g. "required", "min")
	Value any    // actual value
	Param string // tag parameter (e.g. "3" for min=3)
}

// Errors holds all field-level validation failures.
type Errors struct {
	Fields []FieldError
}

func (e *Errors) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		if f.Param != "" {
			parts[i] = fmt.Sprintf("%s failed on '%s=%s'", f.Field, f.Tag, f.Param)
		} else {
			parts[i] = fmt.Sprintf("%s failed on '%s'", f.Field, f.Tag)
		}
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// DefaultValidator is the singleton go-playground validator instance used
// by Struct and (internally) by Den's write path. Exposed so consumers
// that need to register custom validation functions can do so once for
// both surfaces.
var DefaultValidator = validator.New()

// Document validates doc against its `validate` struct tags using
// DefaultValidator. Returns nil on success, *Errors on validation
// failure, or a raw error for malformed input (e.g. a nil pointer).
//
// The parameter type is the marker interface every Den document type
// satisfies by embedding document.Base, so attempts to validate
// arbitrary non-document structs fail at compile time — use
// go-playground/validator/v10 directly for that case.
//
// Den's write path calls Document automatically; this entry point is
// for validating outside the Den boundary (HTTP handlers, form parsers,
// pre-save checks).
func Document(doc document.Document) error {
	err := DefaultValidator.Struct(doc)
	if err == nil {
		return nil
	}
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		return convertErrors(ve)
	}
	return err
}

func convertErrors(ve validator.ValidationErrors) *Errors {
	fields := make([]FieldError, len(ve))
	for i, fe := range ve {
		fields[i] = FieldError{
			Field: fe.Field(),
			Tag:   fe.Tag(),
			Value: fe.Value(),
			Param: fe.Param(),
		}
	}
	return &Errors{Fields: fields}
}
