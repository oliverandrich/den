// Package den is an ODM for Go with two storage backends (SQLite and
// PostgreSQL). It exposes a single chainable QuerySet for filter-and-act
// operations and a small set of doc-in-hand top-level functions for
// persistence (Save, Delete, FindByID, Refresh).
//
// This root package is the convenience surface: type aliases for every
// exported type plus thin wrapper functions for every exported function.
// The implementation lives in github.com/oliverandrich/den/engine, which
// is a public package — applications that need types or functions the
// root does not re-export (e.g. when building tooling) can import it
// directly. The aliases preserve type identity, so `den.QuerySet[T]` IS
// `engine.QuerySet[T]`.
package den

import (
	"context"

	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/engine"
)

// NewID generates a new ULID string. ULIDs are lexicographically sortable
// and timestamp-ordered. Use this for document IDs, worker IDs, or any
// unique identifier.
func NewID() string {
	return engine.NewID()
}

// Open creates a new DB using the given backend directly. The context
// governs any registration work triggered by WithTypes (collection table
// creation, index provisioning); callers with long-running startup work
// can pass a timeout or cancellable context to abort it cleanly.
//
// Use OpenURL for URL-based opening with automatic backend selection.
func Open(ctx context.Context, backend Backend, opts ...Option) (*DB, error) {
	return engine.Open(ctx, backend, opts...)
}

// OpenURL opens a database by DSN, dispatching to the registered backend
// for the scheme (e.g. sqlite:///path.db, postgres://...). The backend
// must be registered first via a side-effect import of its package.
func OpenURL(ctx context.Context, dsn string, opts ...Option) (*DB, error) {
	return engine.OpenURL(ctx, dsn, opts...)
}

// WithTypes queues document types to be registered at the end of Open.
// Equivalent to calling Register(ctx, db, types...) after Open returns.
// Types must embed [document.Base] to satisfy [document.Document] — the
// sealed marker enforces this at compile time.
func WithTypes(types ...document.Document) Option {
	return engine.WithTypes(types...)
}

// WithStorage attaches an attachment Storage to the DB. Required when
// any registered type carries `document.Attachment` fields.
func WithStorage(s Storage) Option {
	return engine.WithStorage(s)
}

// Register registers one or more document types with the database. It
// must be called before any CRUD or query operation on the types. Types
// must embed [document.Base] to satisfy [document.Document] — the sealed
// marker enforces this at compile time.
func Register(ctx context.Context, db *DB, types ...document.Document) error {
	return engine.Register(ctx, db, types...)
}

// Meta returns the registered metadata for type T. Returns
// ErrNotRegistered if T has not been registered.
func Meta[T any](db *DB) (CollectionMeta, error) {
	return engine.Meta[T](db)
}

// Collections returns the names of every registered collection.
func Collections(db *DB) []string {
	return engine.Collections(db)
}

// RegisterBackend registers a backend opener under the given URL scheme.
// Called from backend packages' init() functions; not typically called
// by application code.
func RegisterBackend(scheme string, opener func(ctx context.Context, dsn string) (Backend, error)) {
	engine.RegisterBackend(scheme, opener)
}

// DropStaleIndexes removes indexes that exist on the backend but are no
// longer declared by any registered type. Pass DryRun() to report what
// would change without touching the schema.
func DropStaleIndexes(ctx context.Context, db *DB, opts ...DropStaleOption) (DropStaleResult, error) {
	return engine.DropStaleIndexes(ctx, db, opts...)
}

// DryRun makes DropStaleIndexes report what it would drop without
// touching the schema.
func DryRun() DropStaleOption {
	return engine.DryRun()
}
