package engine

// WithStorage installs a Storage on the DB. Storage is DB-scoped — all
// document types that embed or contain document.Attachment use the same
// backend. Install at Open:
//
//	fs, err := file.New("./uploads", "/media")
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
