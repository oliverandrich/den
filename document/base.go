package document

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

// Base provides the standard fields for all Den documents.
// Embed this in your document structs.
type Base struct {
	ID        string    `json:"_id"`
	CreatedAt time.Time `json:"_created_at"`
	UpdatedAt time.Time `json:"_updated_at"`
	Rev       string    `json:"_rev,omitempty"`
}

// NewID generates a new ULID string suitable for document IDs.
// ULIDs are lexicographically sortable and timestamp-ordered.
func NewID() string {
	return ulid.MustNew(ulid.Now(), rand.Reader).String()
}

// Trackable is implemented by documents that support change tracking.
// den detects this interface after decode and injects the raw JSON snapshot.
type Trackable interface {
	SetSnapshot(data []byte)
	Snapshot() []byte
}

// TrackedBase extends Base with change tracking support.
// Embed this instead of Base for documents that need IsChanged/GetChanges/Rollback.
type TrackedBase struct {
	Base
	snapshot []byte // raw JSON at load time; not serialized
}

// SetSnapshot stores the raw JSON bytes for change detection.
func (b *TrackedBase) SetSnapshot(data []byte) { b.snapshot = data }

// Snapshot returns the raw JSON bytes captured at load time.
func (b *TrackedBase) Snapshot() []byte { return b.snapshot }
