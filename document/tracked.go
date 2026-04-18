package document

// Tracked adds change-tracking support to a document. Embed it alongside
// Base to opt in:
//
//	type Article struct {
//	    document.Base
//	    document.Tracked
//	    Title string `json:"title"`
//	}
//
// Tracked satisfies the Trackable interface so Den populates the snapshot
// byte slice after every load/save. Callers can then use IsChanged,
// GetChanges, and Revert to inspect or undo in-memory modifications.
//
// The snapshot is not serialized — it lives only on the in-memory struct
// and is restored on the next load.
type Tracked struct {
	snapshot []byte // not serialized
}

// SetSnapshot stores the raw JSON bytes for later change detection.
// Called by Den after a load/save.
func (t *Tracked) SetSnapshot(data []byte) { t.snapshot = data }

// Snapshot returns the stored raw JSON bytes, or nil if never loaded.
func (t *Tracked) Snapshot() []byte { return t.snapshot }
