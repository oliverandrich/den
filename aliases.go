package den

import (
	"github.com/oliverandrich/den/engine"
)

// Type aliases for the public surface. Canonical declarations live in
// den/engine (engine internals: DB, Tx, QuerySet, Link, hooks, …) and
// den/backend (the backend contract: Backend, Query, LockMode, …). These
// aliases keep `den.X` as the user-facing identifier regardless of which
// sub-package owns the underlying type.

type (
	DB     = engine.DB
	Tx     = engine.Tx
	Scope  = engine.Scope
	Option = engine.Option

	Backend     = engine.Backend
	ReadWriter  = engine.ReadWriter
	Iterator    = engine.Iterator
	Transaction = engine.Transaction

	Query           = engine.Query
	QuerySet[T any] = engine.QuerySet[T]
	SortEntry       = engine.SortEntry
	SortDirection   = engine.SortDirection

	AggregateOp           = engine.AggregateOp
	GroupByAgg            = engine.GroupByAgg
	GroupByRow            = engine.GroupByRow
	GroupBySortEntry      = engine.GroupBySortEntry
	GroupByBuilder[T any] = engine.GroupByBuilder[T]

	Link[T any] = engine.Link[T]
	LinkRule    = engine.LinkRule

	CRUDOption = engine.CRUDOption
	SetFields  = engine.SetFields

	LockMode   = engine.LockMode
	LockOption = engine.LockOption

	Settings    = engine.Settings
	DenSettable = engine.DenSettable

	CollectionMeta  = engine.CollectionMeta
	FieldMeta       = engine.FieldMeta
	IndexDefinition = engine.IndexDefinition

	BeforeInserter    = engine.BeforeInserter
	AfterInserter     = engine.AfterInserter
	BeforeUpdater     = engine.BeforeUpdater
	AfterUpdater      = engine.AfterUpdater
	BeforeDeleter     = engine.BeforeDeleter
	AfterDeleter      = engine.AfterDeleter
	BeforeSoftDeleter = engine.BeforeSoftDeleter
	AfterSoftDeleter  = engine.AfterSoftDeleter
	BeforeSaver       = engine.BeforeSaver
	AfterSaver        = engine.AfterSaver
	Validator         = engine.Validator

	FieldChange       = engine.FieldChange
	DanglingLinkError = engine.DanglingLinkError

	Storage         = engine.Storage
	SeekableStorage = engine.SeekableStorage
	FTSProvider     = engine.FTSProvider
	FTSSearcher     = engine.FTSSearcher

	DropStaleOption = engine.DropStaleOption
	DropStaleResult = engine.DropStaleResult
	StaleIndex      = engine.StaleIndex
	RecordedIndex   = engine.RecordedIndex
)

// Constants re-exported from den/engine.

const (
	// SortDirection
	Asc  = engine.Asc
	Desc = engine.Desc

	// AggregateOp
	OpSum   = engine.OpSum
	OpAvg   = engine.OpAvg
	OpMin   = engine.OpMin
	OpMax   = engine.OpMax
	OpCount = engine.OpCount

	// LinkRule
	LinkIgnore = engine.LinkIgnore
	LinkWrite  = engine.LinkWrite
	LinkDelete = engine.LinkDelete

	// LockMode
	LockDefault    = engine.LockDefault
	LockSkipLocked = engine.LockSkipLocked
	LockNoWait     = engine.LockNoWait

	// Reserved field name constants
	FieldID           = engine.FieldID
	FieldCreatedAt    = engine.FieldCreatedAt
	FieldUpdatedAt    = engine.FieldUpdatedAt
	FieldRev          = engine.FieldRev
	FieldDeletedAt    = engine.FieldDeletedAt
	FieldDeletedBy    = engine.FieldDeletedBy
	FieldDeleteReason = engine.FieldDeleteReason
)
