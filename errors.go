package den

import (
	"github.com/oliverandrich/den/engine"
)

// Error sentinels re-exported from den/engine so callers keep matching
// via `errors.Is(err, den.ErrNotFound)`.
var (
	ErrNotFound                = engine.ErrNotFound
	ErrMultipleMatches         = engine.ErrMultipleMatches
	ErrDuplicate               = engine.ErrDuplicate
	ErrRevisionConflict        = engine.ErrRevisionConflict
	ErrNotRegistered           = engine.ErrNotRegistered
	ErrValidation              = engine.ErrValidation
	ErrTransactionFailed       = engine.ErrTransactionFailed
	ErrNoSnapshot              = engine.ErrNoSnapshot
	ErrMigrationFailed         = engine.ErrMigrationFailed
	ErrLocked                  = engine.ErrLocked
	ErrDeadlock                = engine.ErrDeadlock
	ErrSerialization           = engine.ErrSerialization
	ErrFTSNotSupported         = engine.ErrFTSNotSupported
	ErrLockRequiresTransaction = engine.ErrLockRequiresTransaction
	ErrIncompatiblePagination  = engine.ErrIncompatiblePagination
	ErrUnsupportedScheme       = engine.ErrUnsupportedScheme
)
