package den

import (
	"context"
	"sync"

	"github.com/oliverandrich/den/id"
	"github.com/oliverandrich/den/internal"
)

// NewID generates a new ULID string. ULIDs are lexicographically sortable
// and timestamp-ordered. Use this for document IDs, worker IDs, or any
// unique identifier.
func NewID() string {
	return id.New()
}

// Option configures a DB during Open.
type Option func(*DB)

// DB is the main entry point for Den operations.
// It wraps a Backend and holds the collection registry.
type DB struct {
	backend          Backend
	collections      map[string]*collectionInfo
	typeToCollection map[string]string // Go type derived name → registered collection name
	typeCache        sync.Map          // reflect.Type → *collectionInfo (lock-free fast path)
	encoder          Encoder
	encoderOnce      sync.Once
	tagValidator     func(doc any) error
	pendingTypes     []any // queued by WithTypes, registered at the end of Open
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

// Open creates a new DB using the given backend directly.
// Use OpenURL for URL-based opening with automatic backend selection.
func Open(backend Backend, opts ...Option) (*DB, error) {
	db := &DB{
		backend:          backend,
		collections:      make(map[string]*collectionInfo),
		typeToCollection: make(map[string]string),
	}
	for _, opt := range opts {
		opt(db)
	}
	if len(db.pendingTypes) > 0 {
		types := db.pendingTypes
		db.pendingTypes = nil
		if err := Register(context.Background(), db, types...); err != nil {
			return nil, err
		}
	}
	return db, nil
}

// WithTypes queues document types to be registered at the end of Open.
// Equivalent to calling Register(context.Background(), db, types...) after
// Open returns, but lets the whole setup read as a single expression:
//
//	db, err := den.OpenURL(dsn, den.WithTypes(&Note{}, &Tag{}))
//
// Any registration error aborts Open and is surfaced as its error. Use
// Register directly when you need to supply a specific context.
func WithTypes(types ...any) Option {
	return func(db *DB) {
		db.pendingTypes = append(db.pendingTypes, types...)
	}
}

// SetTagValidator registers a function that validates documents using struct tags.
// Called automatically before insert and update operations.
func (db *DB) SetTagValidator(fn func(any) error) {
	db.tagValidator = fn
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
