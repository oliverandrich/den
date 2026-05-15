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

// Document is the marker interface every Den document type satisfies by
// embedding Base. The unexported method anchors it to this package so
// only types that compose Base can be Documents — accidental random
// structs cannot satisfy the contract.
//
// Used as the parameter type on entry points that should only accept
// document values (e.g. validate.Document).
type Document interface {
	denDocument()
}

// denDocument is the marker that Base contributes to every embedder.
func (Base) denDocument() {}

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
