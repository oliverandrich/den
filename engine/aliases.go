package engine

import (
	"github.com/oliverandrich/den/backend"
	"github.com/oliverandrich/den/lock"
	"github.com/oliverandrich/den/maintenance"
	"github.com/oliverandrich/den/search"
	"github.com/oliverandrich/den/storage"
)

// Re-exports from den's themed sub-packages: type aliases, constant
// aliases, and one-line wrappers for the option constructors. Each
// `engine.X` resolves to the canonical `<subpackage>.X`, so engine
// internal code can use the bare identifier without an import qualifier
// and so the den root continues to alias `engine.X` without caring which
// sub-package owns the type.

// --- den/backend ---

type (
	Backend     = backend.Backend
	ReadWriter  = backend.ReadWriter
	Transaction = backend.Transaction
	Iterator    = backend.Iterator

	Query            = backend.Query
	SortEntry        = backend.SortEntry
	SortDirection    = backend.SortDirection
	AggregateOp      = backend.AggregateOp
	GroupByAgg       = backend.GroupByAgg
	GroupByRow       = backend.GroupByRow
	GroupBySortEntry = backend.GroupBySortEntry

	LockMode = backend.LockMode

	IndexDefinition = backend.IndexDefinition
	RecordedIndex   = backend.RecordedIndex
	CollectionMeta  = backend.CollectionMeta
	FieldMeta       = backend.FieldMeta
)

const (
	Asc  = backend.Asc
	Desc = backend.Desc

	OpSum   = backend.OpSum
	OpAvg   = backend.OpAvg
	OpMin   = backend.OpMin
	OpMax   = backend.OpMax
	OpCount = backend.OpCount

	LockDefault    = backend.LockDefault
	LockSkipLocked = backend.LockSkipLocked
	LockNoWait     = backend.LockNoWait
)

// --- den/storage ---

type (
	Storage         = storage.Storage
	SeekableStorage = storage.SeekableStorage
)

// --- den/search ---

type (
	FTSSearcher = search.FTSSearcher
	FTSProvider = search.FTSProvider
)

// --- den/lock ---

// LockOption configures a lock acquisition (LockByID, QuerySet.ForUpdate).
type LockOption = lock.Option

// SkipLocked makes a lock acquisition return ErrNotFound immediately if
// another transaction already holds the row lock, instead of blocking.
// Maps to PostgreSQL's FOR UPDATE SKIP LOCKED; on SQLite this option
// is a no-op. Passing both SkipLocked and NoWait is an error — they are
// mutually exclusive in PostgreSQL. Thin wrapper over [lock.SkipLocked].
func SkipLocked() LockOption { return lock.SkipLocked() }

// NoWait makes a lock acquisition return ErrLocked immediately if
// another transaction already holds the row lock, instead of blocking.
// Maps to PostgreSQL's FOR UPDATE NOWAIT; on SQLite this option is a
// no-op. Passing both SkipLocked and NoWait is an error — they are
// mutually exclusive in PostgreSQL. Thin wrapper over [lock.NoWait].
func NoWait() LockOption { return lock.NoWait() }

// --- den/maintenance ---

type (
	DropStaleOption = maintenance.Option
	DropStaleResult = maintenance.DropStaleResult
	StaleIndex      = maintenance.StaleIndex
)

// DryRun causes DropStaleIndexes to report the indexes that would be
// dropped without actually dropping them. Thin wrapper over
// [maintenance.DryRun].
func DryRun() DropStaleOption { return maintenance.DryRun() }
