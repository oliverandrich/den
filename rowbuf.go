package den

import "github.com/oliverandrich/den/document"

// decodeIterRow copies bytes from an iterator (whose buffer may be reused),
// decodes them into doc, and captures a snapshot if Trackable.
// This consolidates the make+copy+decode+snapshot pattern used across
// all row-reading code paths and eliminates the double-copy that previously
// happened when captureSnapshot made its own copy.
func decodeIterRow[T any](db *DB, iterBytes []byte, doc *T) error {
	rawCopy := make([]byte, len(iterBytes))
	copy(rawCopy, iterBytes)

	if err := db.decode(rawCopy, doc); err != nil {
		return err
	}

	// Reuse the same copy for the snapshot — no second allocation.
	if t, ok := any(doc).(document.Trackable); ok {
		t.SetSnapshot(rawCopy)
	}

	return nil
}
