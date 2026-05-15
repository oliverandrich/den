package den

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/oliverandrich/den/where"
)

// QuerySet is a lazy, immutable query builder. Chain methods return copies;
// the query is only executed when a terminal method (All, First, Count, etc.)
// is called.
//
// QuerySet binds to a Scope — either a *DB (operating outside a transaction)
// or a *Tx (operating inside RunInTransaction). Row-level locking via
// ForUpdate is only valid on a *Tx scope; calling it on a *DB-bound QuerySet
// defers an error that surfaces on the terminal method.
//
// The zero value is not usable — always obtain a QuerySet via NewQuery.
// Calling terminal methods on a zero-value QuerySet panics because the
// scope reference is nil.
type QuerySet[T any] struct {
	scope          Scope
	conditions     []where.Condition
	sortFields     []SortEntry
	limitN         int
	skipN          int
	afterID        string
	beforeID       string
	fetchMode      fetchMode
	nestDepth      int
	includeDeleted bool
	// lock is set by ForUpdate. Only meaningful when scope is *Tx; terminal
	// methods surface ErrLockRequiresTransaction otherwise.
	lock *LockMode
	// err captures a deferred error from a chainable method (notably
	// ForUpdate with contradictory options, or any use of ForUpdate on a
	// *DB scope at terminal time) so it can surface on the terminal call.
	err error
}

// NewQuery creates a new QuerySet bound to the given scope. Conditions can
// optionally be passed directly. The context is supplied later when a
// terminal method (All, First, Iter, …) runs, so the same QuerySet can be
// executed against different contexts.
//
// Pass a *DB for queries outside a transaction, or a *Tx from within a
// RunInTransaction closure for a query that sees the transaction's view of
// the data. Use ForUpdate only on a *Tx-bound QuerySet.
func NewQuery[T any](scope Scope, conditions ...where.Condition) QuerySet[T] {
	qs := QuerySet[T]{scope: scope, nestDepth: defaultNestingDepth}
	if len(conditions) > 0 {
		qs.conditions = conditions
	}
	return qs
}

// defaultNestingDepth caps recursive link resolution when the caller
// hasn't set WithNestingDepth explicitly. Used by every read path that
// hydrates links (QuerySet terminals + the CRUD-style read APIs that
// honor `den:"eager"`).
const defaultNestingDepth = 3

// --- Chain methods (return copies) ---

// Where adds filter conditions. Multiple calls are ANDed.
func (qs QuerySet[T]) Where(conditions ...where.Condition) QuerySet[T] {
	qs.conditions = append(slices.Clone(qs.conditions), conditions...)
	return qs
}

// Sort adds a sort criterion. Multiple calls define tie-breakers.
//
// Honored by terminals that return ordered rows: All, AllWithCount, First,
// Iter, Search, Update, and Project. On GroupBy.Into, Sort is honored when
// the referenced field matches a group key; a non-key field returns an
// error — use GroupByBuilder.OrderByAgg for aggregate ordering. Ignored by
// Count, Exists, and the scalar aggregates (Avg / Sum / Min / Max) — those
// operate on unordered sets where sort order has no effect on the result.
func (qs QuerySet[T]) Sort(field string, dir SortDirection) QuerySet[T] {
	qs.sortFields = append(slices.Clone(qs.sortFields), SortEntry{Field: field, Dir: dir})
	return qs
}

// Limit sets the maximum number of results.
//
// Honored by the same row-returning terminals as Sort, plus GroupBy.Into
// (caps the number of group rows returned): All, AllWithCount (data slice
// only; the count path runs unpaginated), First (which rewrites Limit to 1
// internally), Iter, Search, Update, Project, and GroupBy.Into. Ignored by
// Count, Exists, and scalar aggregates — those always operate on the full
// WHERE-filtered set.
func (qs QuerySet[T]) Limit(n int) QuerySet[T] {
	qs.limitN = n
	return qs
}

// Skip sets the number of results to skip (offset pagination).
//
// Honored by the same terminals as Limit (including GroupBy.Into). Ignored
// by Count, Exists, and scalar aggregates.
//
// Cannot be combined with After or Before (cursor pagination) — terminal
// methods return ErrIncompatiblePagination when both styles are set.
func (qs QuerySet[T]) Skip(n int) QuerySet[T] {
	qs.skipN = n
	return qs
}

// After sets the cursor for forward pagination.
//
// Cannot be combined with Skip (offset pagination) — terminal methods return
// ErrIncompatiblePagination when both styles are set.
func (qs QuerySet[T]) After(id string) QuerySet[T] {
	qs.afterID = id
	return qs
}

// Before sets the cursor for backward pagination.
//
// Cannot be combined with Skip (offset pagination) — terminal methods return
// ErrIncompatiblePagination when both styles are set.
func (qs QuerySet[T]) Before(id string) QuerySet[T] {
	qs.beforeID = id
	return qs
}

// fetchMode selects which Link[T] fields a terminal hydrates: only
// `den:"eager"`-tagged fields (fetchDefault, the zero value), every
// Link[T] field (fetchAll, set by WithFetchLinks), or none of them
// (fetchNone, set by WithoutFetchLinks).
type fetchMode int

const (
	fetchDefault fetchMode = iota
	fetchAll
	fetchNone
)

// WithFetchLinks hydrates every Link[T] field on the returned documents,
// regardless of whether the field is tagged with `den:"eager"`.
//
// Honored only by terminals that return *T values: All, AllWithCount, First,
// Iter, and Search. Every other terminal — counts, aggregates, projections,
// GroupBy.Into, and bulk Update — ignores it because it has no documents to
// attach the resolved links to. See Update's godoc for the hook-visibility
// caveat that follows from this rule.
func (qs QuerySet[T]) WithFetchLinks() QuerySet[T] {
	qs.fetchMode = fetchAll
	return qs
}

// WithoutFetchLinks suppresses link hydration on this query, including
// fields tagged `den:"eager"`. Use it when the eager tags would
// otherwise pay a per-link round-trip cost the caller does not need
// (bulk export, IDs-only sweep, count-by-link). Returned `Link[T]`
// values carry their ID but `Value` stays `nil`.
func (qs QuerySet[T]) WithoutFetchLinks() QuerySet[T] {
	qs.fetchMode = fetchNone
	return qs
}

// shouldHydrate reports whether the QuerySet's fetch mode would hydrate
// at least one Link[T] field on T. The result is the routing decision
// for All / AllWithCount / Search (drain to slice + run batched
// resolver vs stream per-row through Iter) and the per-row gate inside
// Iter (skip the resolver call entirely when nothing would be hydrated).
func (qs QuerySet[T]) shouldHydrate() bool {
	switch qs.fetchMode {
	case fetchAll:
		return true
	case fetchNone:
		return false
	case fetchDefault:
		var zero T
		return hasEagerLinkFields(reflect.TypeOf(zero))
	}
	return false
}

// WithNestingDepth caps recursive link resolution. Meaningful for any
// query that hydrates links — `den:"eager"`-tagged fields under the
// default mode, or every link field under WithFetchLinks.
//
// Honored uniformly by every terminal that returns *T values: All,
// AllWithCount, First, Search, and Iter all route through the same
// batched resolver, which recurses level-by-level up to depth. Terminals
// that don't return *T values (Count, Exists, scalar aggregates,
// GroupBy.Into, bulk Update) ignore the setting because they have no
// documents to attach resolved links to.
//
// depth <= 0 disables link hydration entirely, regardless of fetchMode.
// Prefer WithoutFetchLinks if that's the intent.
func (qs QuerySet[T]) WithNestingDepth(depth int) QuerySet[T] {
	qs.nestDepth = depth
	return qs
}

// IncludeDeleted includes soft-deleted documents in the results.
func (qs QuerySet[T]) IncludeDeleted() QuerySet[T] {
	qs.includeDeleted = true
	return qs
}

// ForUpdate acquires a row-level lock on every matching document, held until
// the enclosing transaction commits or rolls back. Only valid on a QuerySet
// bound to a *Tx — on a *DB-bound QuerySet the call is accepted but the
// terminal method will return ErrLockRequiresTransaction.
//
// Pass SkipLocked to omit locked rows from the result set (queue-consumer
// pattern) or NoWait to fail immediately with ErrLocked when a row is held
// by another transaction. On SQLite these options are no-ops because
// IMMEDIATE transactions already serialize writers.
//
// Passing both SkipLocked and NoWait is a programmer error (PG allows only
// one); ForUpdate captures the error on the query set and surfaces it when
// a terminal method runs.
func (qs QuerySet[T]) ForUpdate(opts ...LockOption) QuerySet[T] {
	cfg := lockConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	mode, err := cfg.resolve()
	if err != nil {
		qs.err = err
		return qs
	}
	qs.lock = &mode
	return qs
}

// --- Terminal methods (execute the query) ---

// preflight checks deferred errors set by chain methods and enforces the
// ForUpdate-requires-*Tx and cursor-vs-offset-pagination constraints. Every
// terminal method calls this first so the errors surface consistently.
func (qs QuerySet[T]) preflight() error {
	if qs.err != nil {
		return qs.err
	}
	if qs.lock != nil {
		if _, ok := qs.scope.(*Tx); !ok {
			return ErrLockRequiresTransaction
		}
	}
	if (qs.afterID != "" || qs.beforeID != "") && qs.skipN > 0 {
		return ErrIncompatiblePagination
	}
	return nil
}

// All executes the query and returns all matching documents.
//
// With WithFetchLinks enabled, All drains the result set first and then
// resolves every link field in batched IN-queries (one per target type per
// nesting level) instead of the per-row Get that streaming .Iter() does.
// For N parents sharing a small set of linked targets this collapses
// N round-trips into one — at the cost of buffering the full result set,
// which is already implicit in .All()'s contract. Callers who need true
// streaming with link resolution should keep using .Iter().
func (qs QuerySet[T]) All(ctx context.Context) ([]*T, error) {
	if err := qs.preflight(); err != nil {
		return nil, err
	}
	if qs.shouldHydrate() {
		return qs.allBatched(ctx)
	}
	// Pre-allocate when limit is known to avoid repeated slice growth.
	var results []*T
	if qs.limitN > 0 {
		results = make([]*T, 0, qs.limitN)
	}
	for doc, err := range qs.Iter(ctx) {
		if err != nil {
			return nil, err
		}
		results = append(results, doc)
	}
	return results, nil
}

// allBatched is the .All() implementation used when shouldHydrate reports
// true. It drains the iterator fully before running the batched link
// resolver so pgx's cursor has released the connection by the time the
// resolver issues its IN-query through the same ReadWriter.
func (qs QuerySet[T]) allBatched(ctx context.Context) ([]*T, error) {
	db := qs.scope.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, err
	}

	q := qs.buildBackendQuery(col)
	rw := qs.scope.readWriter()

	iter, err := rw.Query(ctx, col.meta.Name, q)
	if err != nil {
		return nil, err
	}
	results, err := drainIter[T](ctx, db, iter, qs.limitN)
	_ = iter.Close()
	if err != nil {
		return nil, err
	}

	if err := batchResolveLinks(ctx, db, rw, results, qs.nestDepth, qs.fetchMode); err != nil {
		return nil, err
	}
	return results, nil
}

// First returns the first matching document. Returns ErrNotFound if none match.
func (qs QuerySet[T]) First(ctx context.Context) (*T, error) {
	results, err := qs.Limit(1).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, ErrNotFound
	}
	return results[0], nil
}

// Count returns the number of matching documents.
//
// Limit, Skip, and Sort are ignored — Count always operates on the full
// WHERE-filtered set. After / Before cursor modifiers are honored.
func (qs QuerySet[T]) Count(ctx context.Context) (int64, error) {
	if err := qs.preflight(); err != nil {
		return 0, err
	}
	col, err := collectionFor[T](qs.scope.db())
	if err != nil {
		return 0, err
	}

	q := qs.buildBackendQuery(col)
	return qs.scope.readWriter().Count(ctx, col.meta.Name, q)
}

// Exists returns true if at least one document matches.
//
// Limit, Skip, and Sort are ignored — the backend emits its own LIMIT 1
// internally. After / Before cursor modifiers are honored.
func (qs QuerySet[T]) Exists(ctx context.Context) (bool, error) {
	if err := qs.preflight(); err != nil {
		return false, err
	}
	col, err := collectionFor[T](qs.scope.db())
	if err != nil {
		return false, err
	}

	q := qs.buildBackendQuery(col)
	return qs.scope.readWriter().Exists(ctx, col.meta.Name, q)
}

// AllWithCount returns matching documents and the total unpaginated count.
//
// When the QuerySet is bound to a *DB, the count+query run in a read
// transaction for consistency. When bound to a *Tx, they run through the
// existing transaction and no nested tx is opened.
func (qs QuerySet[T]) AllWithCount(ctx context.Context) ([]*T, int64, error) {
	if err := qs.preflight(); err != nil {
		return nil, 0, err
	}
	db := qs.scope.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, 0, err
	}

	// Build both queries upfront
	countQS := qs
	countQS.limitN = 0
	countQS.skipN = 0
	countQS.sortFields = nil
	countQS.afterID = ""
	countQS.beforeID = ""
	countQ := countQS.buildBackendQuery(col)
	dataQ := qs.buildBackendQuery(col)

	run := func(rw ReadWriter) ([]*T, int64, error) {
		total, err := rw.Count(ctx, col.meta.Name, countQ)
		if err != nil {
			return nil, 0, err
		}

		// Drain the iterator fully BEFORE running link lookups. pgx pins the
		// connection to the open rows; issuing rw.Get while the iterator is
		// live returns "conn busy".
		iter, err := rw.Query(ctx, col.meta.Name, dataQ)
		if err != nil {
			return nil, 0, err
		}
		results, err := drainIter[T](ctx, db, iter, qs.limitN)
		_ = iter.Close()
		if err != nil {
			return nil, 0, err
		}

		if qs.shouldHydrate() {
			if err := batchResolveLinks(ctx, db, rw, results, qs.nestDepth, qs.fetchMode); err != nil {
				return nil, 0, err
			}
		}
		return results, total, nil
	}

	// When already inside a transaction, reuse it.
	if tx, ok := qs.scope.(*Tx); ok {
		return run(tx.tx)
	}

	// DB path: open a read tx so count+query see a consistent snapshot.
	tx, err := db.backend.Begin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("begin read tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	results, total, err := run(tx)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrTransactionFailed, err)
	}
	return results, total, nil
}

// drainIter materializes all remaining rows from iter into a new []*T. The
// iterator is not closed by this helper — callers own its lifetime so a
// post-drain batch link resolver can safely route through the same read
// transaction that owns the iterator without hitting pgx's "conn busy".
//
// Cancelling ctx terminates the drain within at most one row; the check
// runs at the top of each iteration, matching the cancellation contract
// QuerySet.Iter documents.
func drainIter[T any](ctx context.Context, db *DB, iter Iterator, capHint int) ([]*T, error) {
	var results []*T
	if capHint > 0 {
		results = make([]*T, 0, capHint)
	}
	for iter.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		doc := new(T)
		if err := decodeWithSnapshot(db, iter.Bytes(), doc); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		results = append(results, doc)
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// Update applies field updates to every matching document. Returns the
// number of updated documents.
//
// When bound to a *DB, the scan + writes run in a new transaction so the
// batch is atomic. When bound to a *Tx, they run inline in the caller's
// transaction — a per-row failure rolls back the caller's transaction too.
//
// Update is fail-fast: any per-row error (BeforeUpdate hook, validation,
// revision conflict, backend write) stops the loop, rolls back the
// transaction, and returns (0, err). There is no partial commit; no
// AfterUpdate / AfterSave hooks fire for rows that would have come after
// the failure.
//
// Field names in fields (as they appear in the `json` struct tag) are
// validated against the registered struct before the write transaction
// opens — an unknown name returns immediately without opening the tx.
// Callers that want to validate field names at application start can
// iterate Meta[T].Fields.
//
// WithFetchLinks and WithNestingDepth have no effect on Update. The loaded
// docs are loop-local and discarded after the per-row write, so resolving
// links would only be visible to BeforeUpdate / Validate hooks — Update
// keeps that path lean and Link.Value remains unresolved (nil). Hooks that
// need linked data should call FetchLink or FetchAllLinks themselves.
func (qs QuerySet[T]) Update(ctx context.Context, fields SetFields) (int64, error) {
	if err := qs.preflight(); err != nil {
		return 0, err
	}
	db := qs.scope.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return 0, err
	}

	if err := validateSetFields(col, fields); err != nil {
		return 0, err
	}

	q := qs.buildBackendQuery(col)

	var count int64
	body := func(tx *Tx) error {
		// Drain the iterator to completion before issuing any writes. pgx's
		// cursor pins the transaction's connection, so running Update while
		// the cursor is still open would hit "conn busy" on PostgreSQL.
		iter, err := tx.tx.Query(ctx, col.meta.Name, q)
		if err != nil {
			return err
		}
		docs, err := drainIter[T](ctx, db, iter, qs.limitN)
		_ = iter.Close()
		if err != nil {
			return err
		}

		for _, doc := range docs {
			if err := ctx.Err(); err != nil {
				return err
			}
			rv := reflect.ValueOf(doc).Elem()
			if err := applySetFields(rv, col, fields); err != nil {
				return err
			}
			if err := Update(ctx, tx, doc); err != nil {
				return err
			}
			count++
		}
		return nil
	}

	if err := runOnScopeVoid(ctx, qs.scope, body); err != nil {
		return 0, err
	}
	return count, nil
}

// Delete removes every document matching the QuerySet's conditions and
// returns the number of rows affected. The scan + writes run in one
// transaction (or inline in the caller's tx when bound to a *Tx); the
// iterator is drained before any write fires, which is required on
// PostgreSQL because pgx pins the transaction's connection while a
// cursor is open and an in-loop write would surface a "conn busy" error.
//
// Per-row hooks fire: BeforeDelete, then on the soft path BeforeSoftDelete
// → AfterSoftDelete → AfterDelete, on the hard path just AfterDelete.
// Soft-delete docs route through the soft path unless HardDelete() is
// passed via opts.
//
// Fail-fast: a per-row error stops the loop, rolls back the transaction,
// and returns (0, err). No partial commit.
func (qs QuerySet[T]) Delete(ctx context.Context, opts ...CRUDOption) (int64, error) {
	if err := qs.preflight(); err != nil {
		return 0, err
	}
	db := qs.scope.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return 0, err
	}

	q := qs.buildBackendQuery(col)

	var count int64
	body := func(tx *Tx) error {
		iter, err := tx.tx.Query(ctx, col.meta.Name, q)
		if err != nil {
			return err
		}
		docs, err := drainIter[T](ctx, db, iter, qs.limitN)
		_ = iter.Close()
		if err != nil {
			return err
		}

		for _, doc := range docs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := Delete(ctx, tx, doc, opts...); err != nil {
				return err
			}
			count++
		}
		return nil
	}

	if err := runOnScopeVoid(ctx, qs.scope, body); err != nil {
		return 0, err
	}
	return count, nil
}

// UpdateOne atomically finds the single document matching the QuerySet's
// conditions, applies fields, and returns the updated document hydrated
// per the QuerySet's fetchMode / nestDepth settings.
//
// Conditions must identify the document uniquely: returns
// ErrMultipleMatches if more than one matches, ErrNotFound if none match.
// Field names are validated against the registered struct before the
// transaction opens.
//
// Sort / Limit / Skip / After / Before are ignored — UpdateOne is
// single-doc-strict-by-conditions. IncludeDeleted is honored.
func (qs QuerySet[T]) UpdateOne(ctx context.Context, fields SetFields) (*T, error) {
	db := qs.scope.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, err
	}
	if err := validateSetFields(col, fields); err != nil {
		return nil, err
	}

	body := func(tx *Tx) (*T, error) {
		doc, err := findOneStrict[T](ctx, tx, qs.conditions, qs.includeDeleted)
		if err != nil {
			return nil, err
		}
		rv := reflect.ValueOf(doc).Elem()
		if err := applySetFields(rv, col, fields); err != nil {
			return nil, err
		}
		if err := Update(ctx, tx, doc); err != nil {
			return nil, err
		}
		if err := batchResolveLinks(ctx, db, tx.readWriter(), []*T{doc}, qs.nestDepth, qs.fetchMode); err != nil {
			return nil, err
		}
		return doc, nil
	}
	return runOnScope(ctx, qs.scope, body)
}

// UpsertOne atomically finds the single document matching the QuerySet's
// conditions and applies fields, or inserts a new document built from
// defaults with fields applied on top. The second return value reports
// which path ran: true means a new document was inserted, false means an
// existing one was updated.
//
// Conditions must identify the document uniquely: ErrMultipleMatches is
// returned if more than one matches. Concurrent upserts on the same
// missing row rely on a unique constraint to fail one of the inserters
// with ErrDuplicate — there is no internal retry, and no row lock is
// taken on the lookup.
//
// On the miss path, defaults is mutated by Insert (ID, CreatedAt,
// UpdatedAt are populated) and returned as the result. Callers reusing a
// shared defaults template across upserts should pass a fresh value each
// call — a stale ID would otherwise be carried into the next Insert.
//
// Hooks follow the standard Insert / Update order. Exactly one path
// runs. IncludeDeleted is honored on the lookup; Sort/Limit/Skip/After/
// Before are ignored.
func (qs QuerySet[T]) UpsertOne(ctx context.Context, defaults *T, fields SetFields) (*T, bool, error) {
	return qs.upsertOne(ctx, defaults, fields)
}

// GetOrCreate is the fetch-this-row-or-insert-defaults shorthand:
// returns the existing document if conditions match exactly one row,
// otherwise inserts defaults as a new row. Existing rows are not
// modified.
//
// Equivalent to UpsertOne(ctx, defaults, SetFields{}); reach for it when
// the typical "fetch by unique key, create with defaults if missing,
// leave the rest alone" pattern doesn't need post-find field updates.
func (qs QuerySet[T]) GetOrCreate(ctx context.Context, defaults *T) (*T, bool, error) {
	return qs.upsertOne(ctx, defaults, SetFields{})
}

func (qs QuerySet[T]) upsertOne(ctx context.Context, defaults *T, fields SetFields) (*T, bool, error) {
	db := qs.scope.db()
	col, err := collectionFor[T](db)
	if err != nil {
		return nil, false, err
	}
	if err := validateSetFields(col, fields); err != nil {
		return nil, false, err
	}

	body := func(tx *Tx) (upsertResult[T], error) {
		existing, err := findOneStrict[T](ctx, tx, qs.conditions, qs.includeDeleted)
		switch {
		case err == nil:
			rv := reflect.ValueOf(existing).Elem()
			if err := applySetFields(rv, col, fields); err != nil {
				return upsertResult[T]{}, err
			}
			if err := Update(ctx, tx, existing); err != nil {
				return upsertResult[T]{}, err
			}
			if err := batchResolveLinks(ctx, db, tx.readWriter(), []*T{existing}, qs.nestDepth, qs.fetchMode); err != nil {
				return upsertResult[T]{}, err
			}
			return upsertResult[T]{doc: existing, inserted: false}, nil
		case errors.Is(err, ErrNotFound):
			rv := reflect.ValueOf(defaults).Elem()
			if err := applySetFields(rv, col, fields); err != nil {
				return upsertResult[T]{}, err
			}
			if err := Insert(ctx, tx, defaults); err != nil {
				return upsertResult[T]{}, err
			}
			if err := batchResolveLinks(ctx, db, tx.readWriter(), []*T{defaults}, qs.nestDepth, qs.fetchMode); err != nil {
				return upsertResult[T]{}, err
			}
			return upsertResult[T]{doc: defaults, inserted: true}, nil
		default:
			return upsertResult[T]{}, err
		}
	}

	res, err := runOnScope(ctx, qs.scope, body)
	if err != nil {
		return nil, false, err
	}
	return res.doc, res.inserted, nil
}

// BackLinks adds a WHERE filter that matches documents whose link field
// references the given target ID. Equivalent to
// Where(where.Field(fieldName).Eq(targetID)) but reads more clearly when
// the intent is a backwards-link lookup. Chainable: combine with Sort /
// Limit / etc as needed, then call a terminal like All / First / Count.
//
//	houses, err := den.NewQuery[House](db).BackLinks("door", doorID).All(ctx)
func (qs QuerySet[T]) BackLinks(fieldName string, targetID string) QuerySet[T] {
	return qs.Where(where.Field(fieldName).Eq(targetID))
}

// --- Internal helpers ---

// buildBackendQuery converts the QuerySet into a backend Query struct.
func (qs QuerySet[T]) buildBackendQuery(col *collectionInfo) *Query {
	q := &Query{
		Collection: col.meta.Name,
		SortFields: qs.sortFields,
		LimitN:     qs.limitN,
		SkipN:      qs.skipN,
		AfterID:    qs.afterID,
		BeforeID:   qs.beforeID,
		Lock:       qs.lock,
	}

	q.Conditions = append(q.Conditions, qs.allConditions(col)...)

	return q
}

// allConditions returns all conditions including auto-injected soft-delete filter.
func (qs QuerySet[T]) allConditions(col *collectionInfo) []where.Condition {
	conditions := qs.conditions
	if col.meta.HasSoftDelete && !qs.includeDeleted {
		conditions = append(slices.Clone(conditions), where.Field("_deleted_at").IsNil())
	}
	return conditions
}
