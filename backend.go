package den

import (
	"context"

	"github.com/oliverandrich/den/where"
)

// Backend defines the contract that all storage engines must implement.
type Backend interface {
	Get(ctx context.Context, collection, id string) ([]byte, error)
	Put(ctx context.Context, collection, id string, data []byte) error
	Delete(ctx context.Context, collection, id string) error

	Query(ctx context.Context, collection string, q *Query) (Iterator, error)
	Count(ctx context.Context, collection string, q *Query) (int64, error)
	Exists(ctx context.Context, collection string, q *Query) (bool, error)
	Aggregate(ctx context.Context, collection string, op AggregateOp, field string, q *Query) (*float64, error)
	GroupBy(ctx context.Context, collection string, groupField string, aggs []GroupByAgg, q *Query) ([]GroupByRow, error)

	EnsureIndex(ctx context.Context, collection string, idx IndexDefinition) error
	DropIndex(ctx context.Context, collection string, name string) error
	ListRecordedIndexes(ctx context.Context, collection string) ([]RecordedIndex, error)

	EnsureCollection(ctx context.Context, name string, meta CollectionMeta) error
	DropCollection(ctx context.Context, name string) error

	Begin(ctx context.Context, writable bool) (Transaction, error)

	Encoder() Encoder

	Ping(ctx context.Context) error
	Close() error
}

// AggregateOp identifies a SQL aggregate function.
type AggregateOp string

const (
	OpSum   AggregateOp = "SUM"
	OpAvg   AggregateOp = "AVG"
	OpMin   AggregateOp = "MIN"
	OpMax   AggregateOp = "MAX"
	OpCount AggregateOp = "COUNT"
)

// GroupByAgg describes a single aggregate expression in a GROUP BY query.
type GroupByAgg struct {
	Op    AggregateOp
	Field string // source field (ignored for OpCount)
}

// GroupByRow holds one result row from a GROUP BY query.
type GroupByRow struct {
	Key    string    // group key value (text representation)
	Values []float64 // aggregate values, matching GroupByAgg order
}

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

// ReadWriter is the common interface for both Backend and Transaction,
// providing the core CRUD operations that all write paths need.
type ReadWriter interface {
	Get(ctx context.Context, collection, id string) ([]byte, error)
	Put(ctx context.Context, collection, id string, data []byte) error
	Delete(ctx context.Context, collection, id string) error
	Query(ctx context.Context, collection string, q *Query) (Iterator, error)
	Count(ctx context.Context, collection string, q *Query) (int64, error)
	Exists(ctx context.Context, collection string, q *Query) (bool, error)
	Aggregate(ctx context.Context, collection string, op AggregateOp, field string, q *Query) (*float64, error)
	GroupBy(ctx context.Context, collection string, groupField string, aggs []GroupByAgg, q *Query) ([]GroupByRow, error)
}

// Transaction provides CRUD operations within a transaction boundary.
type Transaction interface {
	ReadWriter
	Commit() error
	Rollback() error

	// GetForUpdate reads a document and acquires a row-level lock that
	// persists until the transaction commits or rolls back. On PostgreSQL
	// this maps to SELECT ... FOR UPDATE, optionally with SKIP LOCKED or
	// NOWAIT. On SQLite it is a no-op because IMMEDIATE transactions
	// already serialize writers; the mode parameter is ignored.
	GetForUpdate(ctx context.Context, collection, id string, mode LockMode) ([]byte, error)

	// AdvisoryLock acquires an application-defined lock identified by key
	// that persists until the transaction commits or rolls back. Concurrent
	// transactions attempting to acquire the same key block until the holder
	// ends. Unlike GetForUpdate this does not require a row to exist, so it
	// is suitable for bootstrap paths like coordinating concurrent migration
	// starters before any state row has been written.
	//
	// On PostgreSQL this maps to pg_advisory_xact_lock. On SQLite it is a
	// no-op because IMMEDIATE transactions already serialize writers on the
	// whole database.
	AdvisoryLock(ctx context.Context, key int64) error
}

// LockMode selects the row-locking behavior used by GetForUpdate.
type LockMode int

const (
	// LockDefault acquires the lock and blocks if another transaction holds it.
	LockDefault LockMode = iota
	// LockSkipLocked returns no row (ErrNotFound) if another transaction
	// already holds the lock. Mapped to FOR UPDATE SKIP LOCKED on PostgreSQL.
	LockSkipLocked
	// LockNoWait returns ErrLocked immediately if another transaction
	// already holds the lock. Mapped to FOR UPDATE NOWAIT on PostgreSQL.
	LockNoWait
)

// Iterator provides sequential access to query results.
type Iterator interface {
	Next() bool
	Bytes() []byte
	ID() string
	Err() error
	Close() error
}

// IndexDefinition describes a secondary index on a collection.
type IndexDefinition struct {
	Name   string
	Fields []string
	Unique bool
}

// RecordedIndex describes a secondary index that was previously created by
// Den and is tracked in the backend's metadata table. Managed indexes (such
// as the PostgreSQL GIN index or FTS auxiliary objects) are not recorded.
type RecordedIndex struct {
	Name   string
	Fields []string
	Unique bool
}

// CollectionMeta holds structural metadata for a registered collection.
type CollectionMeta struct {
	Name          string
	Fields        []FieldMeta
	Indexes       []IndexDefinition
	HasSoftDelete bool
}

// FieldMeta describes a single field within a collection.
type FieldMeta struct {
	Name      string
	GoName    string
	Type      string
	Indexed   bool
	Unique    bool
	FTS       bool
	IsPointer bool
}

// SortDirection specifies ascending or descending sort order.
type SortDirection int

const (
	Asc SortDirection = iota
	Desc
)

// SortEntry defines a single sort criterion.
type SortEntry struct {
	Field string
	Dir   SortDirection
}

// Query represents an abstract query that backends translate into
// their native query mechanism.
type Query struct {
	Collection string
	Conditions []where.Condition
	SortFields []SortEntry
	LimitN     int // 0 = no limit
	SkipN      int // 0 = no skip
	AfterID    string
	BeforeID   string
	// Lock requests a row-level lock on every matching row (PostgreSQL
	// only; SQLite ignores it because IMMEDIATE tx already serializes
	// writers). nil means no lock; a non-nil pointer's value selects the
	// lock mode. The pointer form rules out the previously-possible
	// invalid pair of (ForUpdate=false, LockMode!=LockDefault).
	Lock *LockMode
}
