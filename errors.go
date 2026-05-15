package den

import (
	"github.com/oliverandrich/den/internal/core"
)

// Error sentinels re-exported from internal/core so callers keep matching
// via `errors.Is(err, den.ErrNotFound)`.
var (
	ErrNotFound                = core.ErrNotFound
	ErrMultipleMatches         = core.ErrMultipleMatches
	ErrDuplicate               = core.ErrDuplicate
	ErrRevisionConflict        = core.ErrRevisionConflict
	ErrNotRegistered           = core.ErrNotRegistered
	ErrValidation              = core.ErrValidation
	ErrTransactionFailed       = core.ErrTransactionFailed
	ErrNoSnapshot              = core.ErrNoSnapshot
	ErrMigrationFailed         = core.ErrMigrationFailed
	ErrLocked                  = core.ErrLocked
	ErrDeadlock                = core.ErrDeadlock
	ErrSerialization           = core.ErrSerialization
	ErrFTSNotSupported         = core.ErrFTSNotSupported
	ErrLockRequiresTransaction = core.ErrLockRequiresTransaction
	ErrIncompatiblePagination  = core.ErrIncompatiblePagination
	ErrUnsupportedScheme       = core.ErrUnsupportedScheme
)
