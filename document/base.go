package document

import (
	"time"
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

// Trackable is implemented by documents that carry a byte-level snapshot of
// their last-saved state. Embed document.Tracked to satisfy this interface;
// den uses it to populate the snapshot after a load and to detect changes
// (IsChanged, GetChanges, Revert).
//
// SetSnapshot implementations may retain data directly without copying —
// the bytes passed in are caller-owned and stable per Den's Backend
// byte-ownership contract. Callers do not mutate the slice afterwards.
type Trackable interface {
	SetSnapshot(data []byte)
	Snapshot() []byte
}
