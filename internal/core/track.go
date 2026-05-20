package core

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

// captureSnapshot stores data as doc's change-tracking snapshot when doc
// implements Trackable. data is retained directly (no defensive copy) —
// the Backend byte-ownership contract guarantees that bytes returned from
// Get / Iterator.Bytes / GetForUpdate are caller-owned, and bytes produced
// by db.encode are fresh per call.
//
// Use captureSnapshot when you already have encoded bytes in hand (typical
// post-encode-then-Put path). Use decodeWithSnapshot when you have raw
// bytes from a backend read and need to decode them into the doc too.
func captureSnapshot(data []byte, doc any) {
	if t, ok := doc.(document.Trackable); ok {
		t.SetSnapshot(data)
	}
}

// decodeWithSnapshot decodes data into doc and, if doc is Trackable,
// stores the same bytes as its change-tracking snapshot. The bytes are
// retained directly — Backend.Get, Iterator.Bytes, and Transaction.GetForUpdate
// all return caller-owned slices per the Backend byte-ownership contract.
//
// One-call replacement for the common pair `db.decode(data, doc)` followed
// by `captureSnapshot(data, doc)` on a freshly-read document.
func decodeWithSnapshot(db *DB, data []byte, doc any) error {
	if err := db.decode(data, doc); err != nil {
		return err
	}
	captureSnapshot(data, doc)
	return nil
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

// Revert restores the document to its state at load time by decoding the
// stored snapshot back over its fields. Returns ErrNoSnapshot if the
// document was never loaded from the database or does not embed
// document.Tracked.
//
// Named Revert rather than Rollback to avoid name collision with the
// backend transaction's Rollback method — this operation is purely an
// in-memory restore against the document snapshot and has nothing to do
// with transactions.
func Revert[T any](db *DB, doc *T) error {
	t, ok := any(doc).(document.Trackable)
	if !ok || t.Snapshot() == nil {
		return ErrNoSnapshot
	}

	// Zero the doc before decoding so fields that were absent in the
	// snapshot JSON (e.g. nil pointers with `omitempty`, zero-valued
	// nested structs) are reset rather than left at their current
	// non-zero in-memory value. JSON Unmarshal alone would merge into
	// the existing doc, missing this case.
	snap := t.Snapshot()
	*doc = *new(T)
	return decodeWithSnapshot(db, snap, doc)
}
