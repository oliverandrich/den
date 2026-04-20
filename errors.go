package den

import (
	"errors"
	"fmt"
	"strings"
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
	// ErrIncompatibleScope is returned when a CRUDOption demands a scope the
	// caller did not provide (e.g. ContinueOnError requires *DB because the
	// caller's transaction cannot be split into per-document transactions).
	ErrIncompatibleScope = errors.New("den: option not compatible with the provided scope")
	// ErrIncompatibleOptions is returned when two mutually-exclusive
	// CRUDOptions are passed together.
	ErrIncompatibleOptions = errors.New("den: incompatible options combined")
)

// InsertFailure pairs a failed document's position in the input slice with
// the underlying error. Used by InsertManyError.
type InsertFailure struct {
	Index int
	Err   error
}

// InsertManyError aggregates per-document failures from InsertMany when the
// ContinueOnError option is set. Failures are listed in input order.
//
// errors.Is matches any sentinel wrapped by any failure; errors.As on a
// per-failure error returns the wrapped concrete type.
type InsertManyError struct {
	Failures []InsertFailure
}

func (e *InsertManyError) Error() string {
	switch len(e.Failures) {
	case 0:
		return "den: insert many: no failures"
	case 1:
		return fmt.Sprintf("den: insert many: 1 failure (index %d: %v)",
			e.Failures[0].Index, e.Failures[0].Err)
	}
	parts := make([]string, len(e.Failures))
	for i, f := range e.Failures {
		parts[i] = fmt.Sprintf("index %d: %v", f.Index, f.Err)
	}
	return fmt.Sprintf("den: insert many: %d failures (%s)",
		len(e.Failures), strings.Join(parts, "; "))
}

// Unwrap returns the wrapped errors so errors.Is and errors.As traverse
// every failure.
func (e *InsertManyError) Unwrap() []error {
	out := make([]error, len(e.Failures))
	for i, f := range e.Failures {
		out[i] = f.Err
	}
	return out
}
