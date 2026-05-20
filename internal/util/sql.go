package util

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

// IdentifierSegment turns a (possibly dotted) JSON field name into a
// fragment that's safe inside a bare SQL identifier. Dotted JSON paths
// (`profile.bio`) are correct in `json_extract` / `jsonb_extract_path`
// expressions, but break in identifier positions like CREATE INDEX
// names or FTS5 virtual-table column lists where the dot is parsed as
// a `table.column` qualifier. Callers keep the dotted form for the
// extraction expression and use this helper for the identifier.
func IdentifierSegment(jsonName string) string {
	return strings.ReplaceAll(jsonName, ".", "_")
}

// EscapeLike escapes LIKE special characters (%, _, \) in a value.
func EscapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
