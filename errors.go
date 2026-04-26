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
	// ErrIncompatiblePagination is returned by terminal QuerySet methods when
	// the caller mixed cursor pagination (After/Before) with offset pagination
	// (Skip). The two styles have no defined interaction — pick one.
	ErrIncompatiblePagination = errors.New("den: cursor pagination (After/Before) cannot be combined with offset pagination (Skip)")
)

// InsertFailure pairs a failed document's position in the input slice with
// the underlying error. Used by InsertManyError.
type InsertFailure struct {
	Index int
	Err   error
}

// insertManyErrorRenderCap bounds how many per-failure detail entries
// Error() renders. Keeps the message usable in logs even when thousands of
// failures accumulated. Remaining failures are reported as "and N more".
const insertManyErrorRenderCap = 10

// InsertManyError aggregates per-document failures from InsertMany when the
// ContinueOnError option is set. Failures are listed in input order.
//
// Failures may be shorter than TotalFailures when the caller caps the
// recorded list via MaxRecordedFailures; Truncated signals that case so
// callers can distinguish "exhaustive list" from "first-N sample". Error()
// and errors.Is/As both respect the cap: only the recorded Failures are
// walked.
//
// errors.Is matches any sentinel wrapped by any recorded failure; errors.As
// on a per-failure error returns the wrapped concrete type.
type InsertManyError struct {
	Failures      []InsertFailure
	Truncated     bool
	TotalFailures int
}

func (e *InsertManyError) Error() string {
	switch e.TotalFailures {
	case 0:
		return "den: insert many: no failures"
	case 1:
		return fmt.Sprintf("den: insert many: 1 failure (index %d: %v)",
			e.Failures[0].Index, e.Failures[0].Err)
	}

	rendered := e.Failures
	if len(rendered) > insertManyErrorRenderCap {
		rendered = rendered[:insertManyErrorRenderCap]
	}
	parts := make([]string, len(rendered))
	for i, f := range rendered {
		parts[i] = fmt.Sprintf("index %d: %v", f.Index, f.Err)
	}

	details := strings.Join(parts, "; ")
	remaining := e.TotalFailures - len(rendered)
	if remaining > 0 {
		return fmt.Sprintf("den: insert many: %d failures (%s; and %d more)",
			e.TotalFailures, details, remaining)
	}
	return fmt.Sprintf("den: insert many: %d failures (%s)", e.TotalFailures, details)
}

// Unwrap returns the wrapped errors so errors.Is and errors.As traverse
// every recorded failure. A fresh slice is allocated on each call so
// callers that mutate Failures see consistent unwrap output afterward —
// the previous sync.Once cache made the slice silently stale on mutation.
// The cost is one O(len(Failures)) allocation per call, sub-microsecond
// at the default MaxRecordedFailures cap of 100.
//
// When Truncated is true, only the recorded Failures are unwrapped; elided
// failures are not reachable via the errors tree. This is intentional — a
// sampled error should not silently appear exhaustive.
func (e *InsertManyError) Unwrap() []error {
	out := make([]error, len(e.Failures))
	for i, f := range e.Failures {
		out[i] = f.Err
	}
	return out
}
