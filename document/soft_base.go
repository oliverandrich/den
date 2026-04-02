package document

import "time"

// SoftBase extends Base with soft delete support.
// Embed this instead of Base for documents that should be soft-deleted.
type SoftBase struct {
	DeletedAt *time.Time `json:"_deleted_at,omitempty"`
	Base
}

// IsDeleted reports whether this document has been soft-deleted.
func (s SoftBase) IsDeleted() bool {
	return s.DeletedAt != nil
}

// TrackedSoftBase extends SoftBase with change tracking support.
// Embed this for documents that need both soft-delete and change tracking.
type TrackedSoftBase struct {
	SoftBase
	snapshot []byte
}

// SetSnapshot stores the raw JSON bytes for change detection.
func (b *TrackedSoftBase) SetSnapshot(data []byte) { b.snapshot = data }

// Snapshot returns the raw JSON bytes captured at load time.
func (b *TrackedSoftBase) Snapshot() []byte { return b.snapshot }
