package internal

import "strings"

// SanitizeFieldName strips characters that are not safe for JSON path interpolation.
// Allows letters, digits, underscores, and dots (for nested paths).
func SanitizeFieldName(field string) string {
	var b strings.Builder
	for _, r := range field {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// EscapeLike escapes LIKE special characters (%, _, \) in a value.
func EscapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
