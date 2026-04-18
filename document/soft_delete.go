package document

import "time"

// SoftDelete adds a `_deleted_at` timestamp to a document. Embed it
// alongside Base to opt into soft-delete semantics:
//
//	type Article struct {
//	    document.Base
//	    document.SoftDelete
//	    Title string `json:"title"`
//	}
//
// When a document with SoftDelete embedded is Delete()d, Den records the
// deletion timestamp instead of removing the row. QuerySet auto-filters
// rows with DeletedAt set unless IncludeDeleted() is used. HardDelete()
// bypasses the soft path and physically removes the row.
//
// Den detects soft-delete support structurally via the `_deleted_at` JSON
// field — any type carrying that field (through this embed or otherwise)
// participates in soft-delete handling.
type SoftDelete struct {
	DeletedAt *time.Time `json:"_deleted_at,omitempty"`
}

// IsDeleted reports whether the document has been soft-deleted.
func (s SoftDelete) IsDeleted() bool {
	return s.DeletedAt != nil
}
