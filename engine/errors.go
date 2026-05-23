package engine

import (
	"errors"
	"fmt"
)

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
	// ErrIncompatiblePagination is returned by terminal QuerySet methods when
	// the caller mixed cursor pagination (After/Before) with offset pagination
	// (Skip). The two styles have no defined interaction — pick one.
	ErrIncompatiblePagination = errors.New("den: cursor pagination (After/Before) cannot be combined with offset pagination (Skip)")
	// ErrUnsupportedScheme is returned by OpenURL when no backend opener is
	// registered for the DSN's scheme — typically because the caller forgot
	// the side-effect import (e.g. `_ "github.com/oliverandrich/den/backend/sqlite"`).
	// Wrapped with the actual scheme via fmt.Errorf so callers can use
	// errors.Is to detect this case without scraping error strings.
	ErrUnsupportedScheme = errors.New("den: unsupported database scheme")
)

// DanglingLinkError describes a Link[T] whose ID does not resolve to any
// row in the target collection. Returned by the batched link-resolver
// when a parent references a deleted or never-existed target. Wraps
// ErrNotFound so callers can keep the simple `errors.Is(err, ErrNotFound)`
// check, but also exposes Collection and ID for callers that need to
// surface "which link broke" without parsing the error message.
type DanglingLinkError struct {
	Collection string
	ID         string
}

func (e *DanglingLinkError) Error() string {
	return fmt.Sprintf("%s: %s id=%q", ErrNotFound, e.Collection, e.ID)
}

func (e *DanglingLinkError) Unwrap() error {
	return ErrNotFound
}
