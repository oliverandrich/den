package den

import "errors"

var (
	ErrNotFound          = errors.New("den: document not found")
	ErrDuplicate         = errors.New("den: duplicate key")
	ErrRevisionConflict  = errors.New("den: revision conflict")
	ErrNotRegistered     = errors.New("den: document type not registered")
	ErrValidation        = errors.New("den: validation failed")
	ErrTransactionFailed = errors.New("den: transaction failed")
	ErrNoSnapshot        = errors.New("den: no snapshot — document was never loaded from database")
	ErrMigrationFailed   = errors.New("den: migration failed")
)
