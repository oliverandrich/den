package den

import (
	"context"
	"sync"

	"github.com/oliverandrich/den/internal"
)

// DB is the main entry point for Den operations.
// It wraps a Backend and holds the collection registry.
type DB struct {
	backend          Backend
	collections      map[string]*collectionInfo
	typeToCollection map[string]string // Go type derived name → registered collection name
	typeCache        sync.Map          // reflect.Type → *collectionInfo (lock-free fast path)
	encoder          Encoder
	encoderOnce      sync.Once
	mu               sync.RWMutex
}

// collectionInfo is the internal registry entry for a registered type.
type collectionInfo struct {
	meta       CollectionMeta
	structInfo *internal.StructInfo
	settings   Settings
}

// Encoder serializes and deserializes documents for a specific backend.
// Each backend provides its own implementation.
type Encoder interface {
	Encode(v any) ([]byte, error)
	Decode(data []byte, v any) error
}

// Open creates a new DB using the given backend.
func Open(backend Backend) (*DB, error) {
	return &DB{
		backend:          backend,
		collections:      make(map[string]*collectionInfo),
		typeToCollection: make(map[string]string),
	}, nil
}

// Close closes the database and its underlying backend.
func (db *DB) Close() error {
	return db.backend.Close()
}

// Backend returns the underlying backend. Useful for advanced use cases
// or backend-specific type assertions.
func (db *DB) Backend() Backend {
	return db.backend
}

// Ping verifies the backend is reachable and operational.
func (db *DB) Ping(ctx context.Context) error {
	return db.backend.Ping(ctx)
}
