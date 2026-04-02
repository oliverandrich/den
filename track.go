package den

import (
	"bytes"
	"fmt"
	"reflect"

	"github.com/oliverandrich/den/document"
)

// FieldChange holds the before and after values for a changed field.
type FieldChange struct {
	Before any
	After  any
}

// captureSnapshot injects the raw JSON bytes into doc if it implements Trackable.
func captureSnapshot(data []byte, doc any) {
	if t, ok := doc.(document.Trackable); ok {
		snapshot := make([]byte, len(data))
		copy(snapshot, data)
		t.SetSnapshot(snapshot)
	}
}

// IsChanged reports whether the document has changed since it was loaded.
// Returns false if the document has no snapshot (never loaded or not Trackable).
func IsChanged[T any](db *DB, doc *T) (bool, error) {
	t, ok := any(doc).(document.Trackable)
	if !ok || t.Snapshot() == nil {
		return false, nil
	}

	current, err := db.encode(doc)
	if err != nil {
		return false, fmt.Errorf("encode for change detection: %w", err)
	}

	return !bytes.Equal(t.Snapshot(), current), nil
}

// GetChanges returns a map of field names to their before/after values
// for all fields that changed since the document was loaded.
// Returns nil if nothing changed or no snapshot exists.
func GetChanges[T any](db *DB, doc *T) (map[string]FieldChange, error) {
	t, ok := any(doc).(document.Trackable)
	if !ok || t.Snapshot() == nil {
		return nil, nil
	}

	var oldMap map[string]any
	if err := db.decode(t.Snapshot(), &oldMap); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}

	// Encode current state to map via JSON roundtrip (single encode+decode)
	var currentMap map[string]any
	if err := encodeToMap(db, doc, &currentMap); err != nil {
		return nil, err
	}

	changes := make(map[string]FieldChange)
	for k, cv := range currentMap {
		ov, exists := oldMap[k]
		if !exists || !reflect.DeepEqual(ov, cv) {
			changes[k] = FieldChange{Before: ov, After: cv}
		}
	}
	for k, ov := range oldMap {
		if _, exists := currentMap[k]; !exists {
			changes[k] = FieldChange{Before: ov, After: nil}
		}
	}

	if len(changes) == 0 {
		return nil, nil
	}
	return changes, nil
}

// encodeToMap encodes a document to map[string]any via a single JSON roundtrip.
func encodeToMap(db *DB, doc any, m *map[string]any) error {
	data, err := db.encode(doc)
	if err != nil {
		return fmt.Errorf("encode for map conversion: %w", err)
	}
	if err := db.decode(data, m); err != nil {
		return fmt.Errorf("decode for map conversion: %w", err)
	}
	return nil
}

// Rollback restores the document to its state at load time.
// Returns ErrNoSnapshot if no snapshot exists.
func Rollback[T any](db *DB, doc *T) error {
	t, ok := any(doc).(document.Trackable)
	if !ok || t.Snapshot() == nil {
		return ErrNoSnapshot
	}

	return db.decode(t.Snapshot(), doc)
}
