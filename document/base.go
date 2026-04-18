package document

import (
	"time"

	"github.com/oliverandrich/den/id"
)

// Base provides the standard fields every Den document carries: ID, creation
// and update timestamps, and an optional revision token for optimistic
// concurrency control. Embed this in your document structs.
//
// Combine with document.SoftDelete for soft-delete support and/or
// document.Tracked for change tracking. The three embeds are orthogonal —
// pick any subset.
type Base struct {
	ID        string    `json:"_id"`
	CreatedAt time.Time `json:"_created_at"`
	UpdatedAt time.Time `json:"_updated_at"`
	Rev       string    `json:"_rev,omitempty"`
}

// NewID generates a new ULID string suitable for document IDs.
func NewID() string {
	return id.New()
}

// Trackable is implemented by documents that carry a byte-level snapshot of
// their last-saved state. Embed document.Tracked to satisfy this interface;
// den uses it to populate the snapshot after a load and to detect changes
// (IsChanged, GetChanges, Revert).
type Trackable interface {
	SetSnapshot(data []byte)
	Snapshot() []byte
}
