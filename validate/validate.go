package validate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/oliverandrich/den"
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

var defaultValidator = validator.New()

// ValidateStruct validates a struct using its `validate` struct tags.
// Returns nil if valid, *Errors if validation fails, or an error for
// invalid input (e.g. nil).
func ValidateStruct(doc any) error {
	return validateWithInstance(defaultValidator, doc)
}

// WithValidation returns a den.Option that enables struct tag validation
// using go-playground/validator. Documents with `validate:"..."` struct tags
// are validated automatically before insert and update operations.
func WithValidation() den.Option {
	v := validator.New()
	return den.WithTagValidator(func(doc any) error {
		return validateWithInstance(v, doc)
	})
}

func validateWithInstance(v *validator.Validate, doc any) error {
	err := v.Struct(doc)
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
