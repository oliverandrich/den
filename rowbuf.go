package den

import "github.com/oliverandrich/den/document"

// decodeIterRow decodes iterBytes into doc and, if doc is Trackable,
// stores iterBytes as its change-tracking snapshot. Per the Backend
// byte-ownership contract, iterBytes is caller-owned and stable beyond
// the next iterator.Next() call, so it can be used directly without a
// defensive copy. Non-Trackable types skip the snapshot entirely.
func decodeIterRow[T any](db *DB, iterBytes []byte, doc *T) error {
	if err := db.decode(iterBytes, doc); err != nil {
		return err
	}
	if t, ok := any(doc).(document.Trackable); ok {
		t.SetSnapshot(iterBytes)
	}
	return nil
}
