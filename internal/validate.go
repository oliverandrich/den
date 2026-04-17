package internal

import (
	"errors"
	"fmt"
	"regexp"
)

// ErrInvalidFieldName signals that a JSON field name does not match the
// allowed identifier pattern. Callers at the public API boundary should
// wrap this with den.ErrValidation so users can errors.Is against that
// sentinel. Kept separate here to avoid an import cycle with the root package.
var ErrInvalidFieldName = errors.New("invalid field name")

// fieldNameRegexp mirrors standard SQL identifier rules: a leading letter or
// underscore, followed by letters, digits, or underscores. Dots are excluded
// here — dotted paths are query-time syntax for nested fields, not struct-tag
// syntax — so json:"a.b" must be rejected at registration time.
var fieldNameRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateFieldName reports whether the given JSON field name is safe to
// interpolate into SQL and consistent with SQL identifier conventions.
// Returns an error wrapping ErrInvalidFieldName on rejection.
func ValidateFieldName(name string) error {
	if !fieldNameRegexp.MatchString(name) {
		return fmt.Errorf("%w: %q must match %s", ErrInvalidFieldName, name, fieldNameRegexp.String())
	}
	return nil
}
