package den

import "errors"

var (
	ErrNotFound          = errors.New("den: document not found")
	ErrMultipleMatches   = errors.New("den: more than one document matched")
	ErrDuplicate         = errors.New("den: duplicate key")
	ErrRevisionConflict  = errors.New("den: revision conflict")
	ErrNotRegistered     = errors.New("den: document type not registered")
	ErrValidation        = errors.New("den: validation failed")
	ErrTransactionFailed = errors.New("den: transaction failed")
	ErrNoSnapshot        = errors.New("den: no snapshot — document was never loaded from database")
	ErrMigrationFailed   = errors.New("den: migration failed")
	ErrLocked            = errors.New("den: row is locked by another transaction")
	ErrDeadlock          = errors.New("den: deadlock detected")
	ErrSerialization     = errors.New("den: serialization failure")
	ErrFTSNotSupported   = errors.New("den: backend does not support full-text search")
	// ErrLockRequiresTransaction is returned when a terminal method runs on a
	// QuerySet whose ForUpdate was set but whose scope is a *DB. Row locking
	// is only meaningful inside a transaction because the lock is released
	// when the enclosing statement commits.
	ErrLockRequiresTransaction = errors.New("den: ForUpdate requires a transaction scope (*Tx)")
)
