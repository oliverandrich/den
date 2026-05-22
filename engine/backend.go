package engine

import (
	"github.com/oliverandrich/den/backend"
)

// Scope is the common parameter type for every CRUD entry point that works
// both outside and inside a transaction. It is sealed to *DB and *Tx — the
// gateway methods are unexported so external types cannot implement it, and
// callers can only obtain a Scope by passing one of the two concrete types.
//
// The idiom mirrors the implicit DBTX pattern used around database/sql
// (where *sql.DB and *sql.Tx share the query surface) but is explicit here
// so the compiler can document and enforce which operations accept either.
type Scope interface {
	readWriter() ReadWriter
	db() *DB
}

// Re-exports of the backend contract types so engine-internal code can use
// the bare identifiers (Backend, Query, LockMode, …) without an import
// qualifier. The canonical declarations live in den/backend.

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
