package den

import "github.com/oliverandrich/den/document"

// decodeIterRow decodes iterBytes into doc and, if doc is Trackable,
// stores iterBytes as its change-tracking snapshot.
//
// Both backend iterators return a freshly-allocated []byte per row
// (pgx Scan and database/sql Scan both document that *[]byte targets
// receive a copy owned by the caller), so the slice is stable beyond
// the next iterator.Next() call and can be used directly — no extra
// make+copy is needed. Non-Trackable types skip the snapshot entirely
// and incur zero rowbuf overhead.
func decodeIterRow[T any](db *DB, iterBytes []byte, doc *T) error {
	if err := db.decode(iterBytes, doc); err != nil {
		return err
	}
	if t, ok := any(doc).(document.Trackable); ok {
		t.SetSnapshot(iterBytes)
	}
	return nil
}
