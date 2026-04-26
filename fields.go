// SPDX-License-Identifier: MIT

package den

// Reserved JSON field names that Den's standard embeds (document.Base
// and document.SoftDelete) install on every registered type. The
// underscore prefix namespaces these away from user-defined fields and
// matches the MongoDB convention.
//
// Use the constants whenever you need the JSON name in code that takes
// a string — `where.Field`, `Sort`, `SetFields`, `After` / `Before`,
// `Project`'s `den:"from:..."` tag — so a refactor stays compile-safe
// instead of relying on string literals scattered across the codebase.
//
// The Go-side struct fields (Base.ID, Base.CreatedAt, …) keep their
// natural names; only the JSON tag (and therefore the SQL column
// access path) uses the underscore form. Storage is independent of
// these constants — renaming would be a breaking storage change, not
// a source rename.
const (
	// FieldID is the document.Base.ID JSON field name. Maps to a
	// 26-character ULID string; sortable chronologically.
	FieldID = "_id"

	// FieldCreatedAt is the document.Base.CreatedAt JSON field name.
	// Set on Insert, never touched afterwards.
	FieldCreatedAt = "_created_at"

	// FieldUpdatedAt is the document.Base.UpdatedAt JSON field name.
	// Refreshed by Insert and Update.
	FieldUpdatedAt = "_updated_at"

	// FieldRev is the document.Base.Rev JSON field name. Present
	// only when the type opts into revision tracking via
	// DenSettings().UseRevision; absent (omitempty) otherwise.
	FieldRev = "_rev"

	// FieldDeletedAt is the document.SoftDelete.DeletedAt JSON field
	// name. Available only on types that embed document.SoftDelete.
	// Default queries auto-filter rows where this is non-nil; opt
	// back in via QuerySet.IncludeDeleted or den.IncludeDeleted as
	// a CRUDOption.
	FieldDeletedAt = "_deleted_at"

	// FieldDeletedBy is the document.SoftDelete.DeletedBy JSON field
	// name. Optional audit value populated via the SoftDeleteBy
	// CRUDOption on the soft-delete path.
	FieldDeletedBy = "_deleted_by"

	// FieldDeleteReason is the document.SoftDelete.DeleteReason JSON
	// field name. Optional audit value populated via the
	// SoftDeleteReason CRUDOption on the soft-delete path.
	FieldDeleteReason = "_delete_reason"
)
