package den

import (
	"context"
	"io"

	"github.com/oliverandrich/den/document"
)

// Storage abstracts the backing byte store for document.Attachment fields.
// Implementations map logical paths to byte streams; they carry no
// knowledge of Den's document metadata (which lives in the backend).
//
// Implementations must be safe for concurrent use.
type Storage interface {
	// Store copies r into the backing store, computes a content hash, and
	// returns a populated Attachment ready to be assigned onto a document
	// before Insert. ext is appended to the generated StoragePath (e.g.
	// ".jpg") — callers derive it from the original filename after any
	// MIME or extension validation. mime annotates the returned
	// Attachment; it is not verified against the content by Storage.
	//
	// Implementations MUST be content-addressed enough that two calls
	// with identical bytes resolve to the same StoragePath; Den relies on
	// that for deduplication via unique indexes on StoragePath.
	Store(ctx context.Context, r io.Reader, ext, mime string) (document.Attachment, error)

	// Open returns a reader for the bytes previously stored under a.StoragePath.
	Open(ctx context.Context, a document.Attachment) (io.ReadCloser, error)

	// Delete removes the bytes at a.StoragePath. Implementations SHOULD
	// treat a missing path as success — cleanup is orchestrated against
	// the document lifecycle and a missing file is the expected terminal
	// state.
	Delete(ctx context.Context, a document.Attachment) error

	// URL returns a URL path (starts with "/") at which a is served.
	// The caller prefixes scheme+host as needed. Remote storages may
	// return an absolute URL instead.
	URL(a document.Attachment) string
}

// WithStorage installs a Storage on the DB. Storage is DB-scoped — all
// document types that embed or contain document.Attachment use the same
// backend. Install at Open:
//
//	fs, err := storage.NewFilesystemStorage("./uploads", "/media")
//	// handle err
//	db, err := den.OpenURL(ctx, dsn, den.WithStorage(fs))
//
// Without a Storage, Den refuses to hard-delete documents that carry
// attachments — orphan bytes are worse than a clear error.
func WithStorage(s Storage) Option {
	return func(db *DB) {
		db.storage = s
	}
}

// Storage returns the Storage configured on db, or nil if none was
// installed. Application code that owns the upload flow (web handlers,
// CLI importers) calls Store directly via this accessor.
func (db *DB) Storage() Storage {
	return db.storage
}
