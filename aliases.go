package den

import (
	"github.com/oliverandrich/den/internal/core"
)

// Type aliases for the public surface. The canonical declarations live in
// internal/core; these aliases keep `den.X` as the user-facing identifier
// while the implementation lives in a single internal package.

type (
	DB     = core.DB
	Tx     = core.Tx
	Scope  = core.Scope
	Option = core.Option

	Backend     = core.Backend
	ReadWriter  = core.ReadWriter
	Iterator    = core.Iterator
	Transaction = core.Transaction

	Query           = core.Query
	QuerySet[T any] = core.QuerySet[T]
	SortEntry       = core.SortEntry
	SortDirection   = core.SortDirection

	AggregateOp           = core.AggregateOp
	GroupByAgg            = core.GroupByAgg
	GroupByRow            = core.GroupByRow
	GroupBySortEntry      = core.GroupBySortEntry
	GroupByBuilder[T any] = core.GroupByBuilder[T]

	Link[T any] = core.Link[T]
	LinkRule    = core.LinkRule

	CRUDOption = core.CRUDOption
	SetFields  = core.SetFields

	LockMode   = core.LockMode
	LockOption = core.LockOption

	Settings    = core.Settings
	DenSettable = core.DenSettable

	CollectionMeta  = core.CollectionMeta
	FieldMeta       = core.FieldMeta
	IndexDefinition = core.IndexDefinition

	BeforeInserter    = core.BeforeInserter
	AfterInserter     = core.AfterInserter
	BeforeUpdater     = core.BeforeUpdater
	AfterUpdater      = core.AfterUpdater
	BeforeDeleter     = core.BeforeDeleter
	AfterDeleter      = core.AfterDeleter
	BeforeSoftDeleter = core.BeforeSoftDeleter
	AfterSoftDeleter  = core.AfterSoftDeleter
	BeforeSaver       = core.BeforeSaver
	AfterSaver        = core.AfterSaver
	Validator         = core.Validator

	FieldChange       = core.FieldChange
	DanglingLinkError = core.DanglingLinkError

	Storage         = core.Storage
	SeekableStorage = core.SeekableStorage
	FTSProvider     = core.FTSProvider
	FTSSearcher     = core.FTSSearcher

	DropStaleOption = core.DropStaleOption
	DropStaleResult = core.DropStaleResult
	StaleIndex      = core.StaleIndex
	RecordedIndex   = core.RecordedIndex
)

// Constants re-exported from internal/core.

const (
	// SortDirection
	Asc  = core.Asc
	Desc = core.Desc

	// AggregateOp
	OpSum   = core.OpSum
	OpAvg   = core.OpAvg
	OpMin   = core.OpMin
	OpMax   = core.OpMax
	OpCount = core.OpCount

	// LinkRule
	LinkIgnore = core.LinkIgnore
	LinkWrite  = core.LinkWrite
	LinkDelete = core.LinkDelete

	// LockMode
	LockDefault    = core.LockDefault
	LockSkipLocked = core.LockSkipLocked
	LockNoWait     = core.LockNoWait

	// Reserved field name constants
	FieldID           = core.FieldID
	FieldCreatedAt    = core.FieldCreatedAt
	FieldUpdatedAt    = core.FieldUpdatedAt
	FieldRev          = core.FieldRev
	FieldDeletedAt    = core.FieldDeletedAt
	FieldDeletedBy    = core.FieldDeletedBy
	FieldDeleteReason = core.FieldDeleteReason
)
