# Changelog

All notable changes to Den are documented here. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## Unreleased

### Breaking Changes

- **`Backend` interface extended** — the `Backend` interface gained a `ListRecordedIndexes(ctx, collection) ([]RecordedIndex, error)` method. Custom backend implementations must add this method. It should return the indexes tracked in the backend's private metadata table (managed indexes such as GIN or FTS auxiliary objects must not be tracked and therefore not returned)
- **`Transaction` interface extended** — the `Transaction` interface gained a `GetForUpdate(ctx, collection, id, mode LockMode) ([]byte, error)` method. Custom transaction implementations must add this method. On PostgreSQL it should emit `SELECT ... FOR UPDATE` (with `SKIP LOCKED` or `NOWAIT` suffix per mode); on serializing-writer backends like SQLite it can delegate to `Get` and ignore the mode
- **`Transaction` interface extended** — the `Transaction` interface gained an `AdvisoryLock(ctx, key int64) error` method. Custom transaction implementations must add this method. On PostgreSQL it should map to `pg_advisory_xact_lock`; on serializing-writer backends like SQLite it can be a no-op since IMMEDIATE transactions already serialize writers
- **`Query` struct locking fields collapsed** — `Query.ForUpdate bool` and `Query.LockMode LockMode` replaced by `Query.Lock *LockMode`. `nil` means no lock; a non-nil pointer's value selects the mode. Custom backends must substitute `q.Lock != nil` for `q.ForUpdate`, and `*q.Lock` for `q.LockMode`. The new shape makes the previously-possible invalid pair `(ForUpdate=false, LockMode!=LockDefault)` unrepresentable
- **`HardDelete` is now a `CRUDOption`** — replaces the top-level `HardDelete[T](ctx, db, doc, opts...)` function. Callers migrate to `Delete(ctx, db, doc, HardDelete())`. The CRUDOption composes with other options (`WithLinkRule`, future options), so `HardDelete` no longer needs to silently inject itself through a private option helper
- **`db.SetTagValidator(fn)` replaced by `WithTagValidator(fn) Option`** — configure tag validation at Open instead of via a post-construction method. Avoids the race window where a concurrent `Register` could race against a late `SetTagValidator`. The `validate.WithValidation()` helper continues to work transparently — it just wraps `WithTagValidator` internally
- **`TxGet` / `TxPut` renamed to `TxRawGet` / `TxRawPut`** — these are raw-bytes escape hatches intended only for infrastructure code (for example, the migration log). The new names make the limited purpose obvious. Callers using them for normal document I/O should migrate to `TxFindByID` / `TxInsert` / `TxUpdate`, which preserve the encoder and registry contract
- **`Open` and `OpenURL` take a leading `context.Context`** — and pass it into backend setup (connection dialing, metadata-table creation, server version check) and any registration work triggered by `WithTypes`. Callers with a startup deadline or cancellable shutdown can now abort the database open cleanly. The backend-opener type registered by `RegisterBackend` also gains a leading `ctx` parameter. Migration: `den.OpenURL(dsn, opts...)` → `den.OpenURL(ctx, dsn, opts...)`; `den.Open(backend, opts...)` → `den.Open(ctx, backend, opts...)`; custom backends must update their `Open` entry point and init-time `RegisterBackend` call to take `ctx`
- **`QuerySet[T]` no longer stores `ctx` in the struct; terminal methods take it as a parameter** — the long-standing Go antipattern of stashing a `context.Context` on a struct was confusing: the `ctx` captured at `NewQuery(ctx, db, …)` silently overrode any deadline a caller might introduce later. `NewQuery[T]` now takes only the `*DB` (plus optional conditions), and every terminal method takes `ctx` as its first argument: `All(ctx)`, `First(ctx)`, `Count(ctx)`, `Exists(ctx)`, `Iter(ctx)`, `AllWithCount(ctx)`, `Update(ctx, fields)`, `Avg(ctx, field)` and the other aggregates, `Search(ctx, queryText)`, `Project(ctx, target)`, `GroupBy(field).Into(ctx, target)`. `TxQuerySet[T]` is unchanged (its `ctx` still flows from the enclosing transaction, which is scoped correctly). Migration is mechanical: drop `ctx, ` from every `NewQuery[T](ctx, db, …)` call and add `ctx` as the first argument to the terminal call

### Added

- **`den.DropStaleIndexes()`** — explicit API for cleaning up indexes that were created by a previous `Register()` but no longer correspond to any `IndexDefinition` in the current struct. Pass `den.DryRun()` to preview the plan without mutating the database. Returns a `DropStaleResult` listing both `Dropped` and `Kept` indexes. Backed by a new `_den_indexes` metadata table created automatically on `Open()` for both SQLite and PostgreSQL
- **`den.TxLockByID[T]()`** — transaction-only API that reads a document and acquires a row-level lock held until the transaction commits or rolls back. On PostgreSQL emits `SELECT ... FOR UPDATE`; on SQLite is a no-op because IMMEDIATE transactions already serialize writers. The `*den.Tx` parameter enforces transaction-only usage at compile time
- **Lock modifiers: `den.SkipLocked()` and `den.NoWait()`** — options for `TxLockByID` that change how contention is handled on PostgreSQL. `SkipLocked` maps to `FOR UPDATE SKIP LOCKED` and returns `ErrNotFound` immediately when another transaction holds the row — the queue-consumer primitive. `NoWait` maps to `FOR UPDATE NOWAIT` and returns the new `ErrLocked` sentinel. Both are no-ops on SQLite. Conflicting options resolve as "last wins"
- **`den.ErrLocked`** — new sentinel error for `TxLockByID` with `NoWait()` when the row is held by another transaction
- **`den.NewTxQuery[T]` and `TxQuerySet[T]`** — transaction-scoped query builder with `ForUpdate(opts ...LockOption)` for multi-row locking. Minimal chainable API (`Where`, `Sort`, `Limit`, `Skip`, `ForUpdate`) plus `All`/`First` terminals. Reuses the `SkipLocked`/`NoWait` options from single-row locking. Only callable via `*den.Tx`, enforcing transaction scope at compile time. `Query` struct gains additive `ForUpdate` and `LockMode` fields
- **`den.ErrDeadlock`** — new sentinel error returned when PostgreSQL reports `40P01 deadlock_detected`. Enables `errors.Is(err, den.ErrDeadlock)` instead of type-switching on pgx internals
- **`den.ErrSerialization`** — new sentinel error returned when PostgreSQL reports `40001 serialization_failure`. Becomes relevant once callers opt into stricter isolation; the sentinel is available now so that upgrade path is straightforward
- **`den.WithTypes(...any) Option`** — register document types at Open. Lets the entire setup read as a single expression: `den.OpenURL(dsn, den.WithTypes(&Note{}, &Tag{}))`. Registration errors abort Open and are surfaced as its error. Use `Register` directly when you need to supply a specific context
- **`den.ErrFTSNotSupported`** — new sentinel error returned by `QuerySet.Search` when the backend does not implement `FTSProvider`. Callers can `errors.Is` against the sentinel instead of pattern-matching on the error string

### Changed

- **Non-blocking PostgreSQL index creation** — `Register()` now emits `CREATE INDEX CONCURRENTLY` for both expression indexes and the auto-created GIN index. Concurrent writes are no longer blocked during index creation on large collections. If a previous concurrent run left an invalid index behind, `EnsureIndex` detects it via `pg_index.indisvalid` and recreates it automatically. SQLite behavior is unchanged
- **`QuerySet.Iter()` terminates on the first error** — previously, if a decode or fetch-links failure happened mid-iteration the loop yielded the error and then continued to the next row. Most `iter.Seq2` producers in the ecosystem stop at the first error, so `Iter()` now matches that convention. Callers doing `for doc, err := range qs.Iter()` should handle the error and exit the loop — further rows will not be yielded after the first error
- **Per-op reflection amortized** — pre-resolved pointers to the embedded base fields (`_id`, `_rev`, `_created_at`, `_updated_at`, `_deleted_at`) are now cached on `StructInfo` during one-time analysis so CRUD, revision, and soft-delete paths use direct `FieldByIndex` instead of per-operation `FieldByName`. `Link[T]` sub-field indices (`ID`, `Value`, `Loaded`) are cached on `linkFieldInfo` the same way, turning each link resolution into index-based access. GROUP BY scan buffers (`scanDest`, `vals`) are hoisted outside the per-row loop on both backends so only their pointer-target slots are reset each iteration. Public API surface unchanged
- **Drain loops consolidated** — the near-duplicated decode-and-collect loops in `NewTxQuery.All`, `BackLinks`, `AllWithCount`, and `Search` now share a single `drainIter[T]` helper. Fewer places to regress when the iteration contract changes
- **Row-decode allocations cut** — `decodeIterRow` no longer pre-copies iterator bytes before decoding. Both backend iterators already return a fresh `[]byte` per row (pgx `Scan` and `database/sql` `Scan` document this contract), so the slice is stable beyond the next `Next()` and is used directly. Non-`Trackable` document types now incur zero rowbuf overhead; `Trackable` types share the same slice as both decode input and snapshot. Micro-benchmark on `QueryAll100` / `QueryIter100` shows ~100 fewer allocations per call (−8%) and ~15% less peak bytes allocated per op
- **PostgreSQL `toJSONBParam` returns `[]byte` instead of `string`** — drops the `string(b)` conversion for every JSONB-cast query parameter. One allocation saved per JSONB parameter; on high-cardinality `Where.In(...)` queries that scales linearly with the number of values. pgx accepts `[]byte` for `::jsonb` casts verbatim

### Fixed

- **Revision check silently skipped when in-memory `_rev` is empty** — `checkAndUpdateRevision` guarded the conflict check with `currentRev != ""`, which caused `Update` of a revisioned document constructed with only an ID (no `_rev`) to silently overwrite the stored document. The guard now keys off document existence (`id != ""`), so an empty in-memory rev against a populated DB rev correctly returns `ErrRevisionConflict`
- **Bulk `QuerySet.Update` deadlocked on PostgreSQL** — the iterator was drained while issuing writes on the same transaction, but `pgx.Rows` pins the connection until closed, so the second statement returned `conn busy`. The implementation now materializes matching documents into a slice, closes the iterator, then runs updates. SQLite behavior is unchanged
- **Unsanitized JSON field names reached SQL construction** — defense-in-depth fix for field names from struct tags. `Register()` now rejects any JSON name that doesn't match `^[A-Za-z_][A-Za-z0-9_]*$` with an error wrapping `den.ErrValidation`. The SQLite FTS column-list path and the PostgreSQL expression-index path also apply `sanitizeFieldName` to every field, closing the raw-interpolation gaps even if a custom pipeline bypassed registration
- **`migrate.Up` TOCTOU on the applied-migrations log** — `loadApplied` read the log outside any transaction, so two processes starting simultaneously both saw the same snapshot and both ran the same pending migration, producing duplicate work and — for non-idempotent forward functions — broken state. The "already applied?" check now happens inside each migration's own transaction, guarded by an advisory lock, so every version runs exactly once across concurrent starters
- **`AllWithCount` with `WithFetchLinks()` exhausted the PostgreSQL connection pool** — the read transaction held one connection for the iterator while each per-row link resolution grabbed a separate pool connection via `db.backend.Get`. With default pool sizing and a handful of concurrent callers every connection was consumed by active iterators plus their link fetches, causing `begin read tx` to time out. Link resolution now routes through the iterator's transaction (iterator is fully drained before link lookups to avoid pgx's "conn busy" on active rows)
- **`SkipLocked()` and `NoWait()` passed together silently let the last-registered option win** — they are mutually exclusive in PostgreSQL, so the previous behavior masked programmer mistakes. `TxLockByID` now returns a clear error on conflict; `TxQuerySet.ForUpdate` (chainable, can't return an error directly) captures the error and surfaces it on the terminal `All`/`First` call
- **Unsorted `NewTxQuery(...).ForUpdate().All()` could deadlock on PostgreSQL** — without an `ORDER BY` clause, two concurrent callers with overlapping result sets acquired row locks in different heap orders and triggered `40P01 deadlock_detected`. `buildSelectSQL` now appends a default `ORDER BY id ASC` when a lock is requested and no explicit sort is set, so every caller walks the lock order identically
- **`mapPGError` now recognizes deadlock and serialization failures** — `40P01` maps to `den.ErrDeadlock` and `40001` maps to `den.ErrSerialization`. Callers previously saw raw pgx errors for these cases, defeating the purpose of sentinel errors
- **`migrate.Down` / `migrate.DownOne` TOCTOU on the applied-migrations log** — symmetric to the `Up` fix: `loadApplied` was read outside any transaction, so two concurrent rollback starters both saw the same applied set and both ran `Backward` for the same version. `runBackward` now acquires the same advisory lock used for forward migrations and re-reads the log inside the transaction, so every version is rolled back exactly once across concurrent starters
- **`Update` / `Delete` on a document without an ID returned a plain `fmt.Errorf`** — callers could not `errors.Is` the failure. Both paths now wrap the sentinel `ErrValidation`, matching the rest of the validation surface
- **`DenSettings()` defined on a pointer receiver was silently ignored when the user passed a value to `Register`** — the direct type assertion against `DenSettable` only matched the exact receiver kind. `getSettings` now retries via a synthesized pointer so settings are picked up regardless of whether the user passed `T{}` or `&T{}`

## 0.7.0 — 2026-04-15

### Breaking Changes

- **`ReadWriter` and `Backend` interfaces extended** — Both interfaces now include a `GroupBy` method for SQL-native group-by aggregation. Custom backend implementations must add this method
- **Dead `Settings` fields removed** — `OmitEmpty`, `UseCache`, `CacheCapacity`, `CacheExpiration`, and `NestingDepthPerField` were declared but never read by any code. They have been removed from the `Settings` struct. If your code set these fields, remove the assignments — they had no effect
- **`ParseDenTag` returns error** — Now returns `(TagOptions, error)` instead of `TagOptions`. Unrecognized tag options produce an error at `Register()` time. If you called `ParseDenTag` directly (unlikely outside Den internals), update the call site
- **`ARCHITECTURE.md` removed** — Documentation now lives exclusively in `docs/` and `llms-full.txt`. If you referenced ARCHITECTURE.md, use the docs site instead

### Added

- **`den.Open()` exported** — allows constructing a `*DB` from a `Backend` instance directly, without going through a URL scheme. Useful for custom or mock backends
- **`omitempty` recognized in den tag** — `den:"omitempty"` is now a valid tag option

### Changed

- **Shared code extracted to internal** — `sanitizeFieldName`, `escapeLike`, and JSON encoding deduplicated from both backends into the `internal` package
- **`Collections()` returns sorted names** — output is now deterministic
- **GroupBy SQL pushdown** — `GroupBy().Into()` now generates a native SQL `GROUP BY` statement instead of loading all documents into memory. This reduces O(N) memory and CPU to a single database query. New `GroupBy` method on the `ReadWriter` interface

### Fixed

- **PostgreSQL type-aware JSONB comparisons** — The PostgreSQL backend now uses `jsonb_extract_path` with `::jsonb` casts instead of `data->>'field'` text extraction. This fixes four related bugs: numeric sorts were lexicographic ("9" sorted after "100"), `Gt`/`Lt` on string fields crashed with `::float` cast, `Eq`/`Ne` used text comparison while `Gt`/`Lt` used float (semantic inconsistency), and nested dot-notation fields like `address.city` silently matched nothing
- **Nested field paths on PostgreSQL** — `where.Field("address.city").Eq("Berlin")` now correctly traverses nested objects using `jsonb_extract_path(data, 'address', 'city')` instead of the broken `data->>'address.city'` literal key lookup
- **Revision check TOCTOU race** — `den.Update()` with revision checking now auto-wraps the revision check and write in a transaction when not already in one, preventing concurrent writers from interleaving on PostgreSQL
- **LinkWrite validation bypass** — Documents written via `WithLinkRule(LinkWrite)` now run both struct tag validation and `Validator.Validate()`, matching the same hook order as direct `Insert`/`Update`
- **Panic in aggregate SQL for unknown ops** — `buildAggregateSQL` in both backends now returns an error instead of panicking on unsupported aggregate operations
- **AllWithCount consistency** — `AllWithCount` now wraps Count and Query in a single read transaction so the total is consistent with results under concurrent writes
- **Unknown den tag options rejected** — `ParseDenTag` now returns an error for unrecognized options (e.g. `den:"indx"`), surfacing typos at `Register()` time
- **Link resolution with custom collection names** — `FetchLink`, `WithLinkRule(LinkWrite)`, and cascade delete now respect custom `CollectionName` from `DenSettings()`

## 0.6.0 — 2026-04-08

### Breaking Changes

- **Hook order reversed around validation** — Mutating hooks (`BeforeInsert`, `BeforeUpdate`, `BeforeSave`) now run **before** both struct tag validation and the `Validator.Validate()` interface. The new insert order is `BeforeInsert → BeforeSave → tag validation → Validate() → write`, matching the pattern used by ActiveRecord, Django ORM, and SQLAlchemy. This lets a `BeforeInsert` hook populate a field that the validator requires — for example, deriving a slug from a title and having the slug marked `validate:"required"`. The previous order ran validation first, which made this pattern impossible.

  **Migration**: if your code relied on `Validate()` running before `BeforeInsert` (unusual — most code wants the opposite), move the check into `BeforeInsert` itself.

## 0.5.0 — 2026-04-06

### Added

- **Composite indexes via struct tags** — `den:"unique_together:group"` and `den:"index_together:group"` allow declarative multi-field indexes. Fields sharing a group name are combined into a single composite index. Both SQLite and PostgreSQL backends generate correct partial indexes with NULL-exclusion WHERE clauses
- **Settings.Indexes now wired up** — `DenSettings().Indexes` was previously declared but never applied during `Register()`. Custom `IndexDefinition` entries are now merged into the collection metadata and created as actual database indexes

## 0.4.2 — 2026-04-06

### Added

- **PostgreSQL version check** — the PostgreSQL backend now verifies the server version on connect and requires PostgreSQL 13 or later. Provides a clear error message instead of cryptic SQL failures on unsupported versions
- **LLM documentation** — `llms.txt` and `llms-full.txt` for AI tool discoverability, following the llms.txt standard

## 0.4.1 — 2026-04-05

### Added

- **Documentation site** — full MkDocs documentation with Material theme, hosted on ReadTheDocs. Covers getting started, guides (CRUD, queries, relations, aggregations, FTS, transactions, hooks, soft delete, change tracking, revision control, validation, migrations, testing), and API reference
- **Third-party licenses** — `scripts/generate-licenses.sh` for automated license generation via `go-licenses`
- **justfile targets** — `just docs` (serve locally), `just docs-build` (static build), `just licenses` (regenerate third-party licenses)
- **ReadTheDocs configuration** — `.readthedocs.yaml` for automated builds via Zensical

## 0.4.0 — 2026-04-05

### Added

- **`den/id` package** — public leaf package for ULID generation (`id.New()`), no framework dependencies. `den.NewID()` and `document.NewID()` both delegate to it. Useful for generating IDs outside of document contexts (e.g. worker IDs, correlation IDs).

## 0.3.0 — 2026-04-05

### Added

- **String matching operators** — `StringContains(substr)`, `StartsWith(prefix)`, `EndsWith(suffix)` for LIKE-based substring matching on string fields, with proper escaping of special characters

## 0.2.1 — 2026-04-05

### Fixed

- **SQLite PRAGMA handling** — user-provided PRAGMAs in the DSN are now preserved; defaults are only applied when not overridden. Previously, passing query parameters caused a malformed DSN with duplicate `?` separators.

### Added

- **SQLite performance PRAGMAs** — added `temp_store(MEMORY)`, `mmap_size(134217728)`, `journal_size_limit(27103364)`, and `cache_size(2000)` as defaults, matching dj-lite and Burrow's recommended configuration

## 0.2.0 — 2026-04-05

### Breaking Changes

- **`den.Open(backend)` replaced by `den.OpenURL(dsn)`** — URL-based opening with automatic scheme detection. Backend packages now register via `init()` and are imported with `_` for side effects. `den.Open` is unexported.
  - `sqlite:///path/to/db` for SQLite
  - `sqlite://:memory:` for in-memory SQLite
  - `postgres://user:pass@host/db` for PostgreSQL

### Added

- **Benchmark suite** — per-operation benchmarks for both backends covering Insert, FindByID, QueryAll, QueryIter, Update, Delete, and QueryWithCondition with `just bench` recipe

### Changed

- **Reduced allocations on hot paths** — cached `reflect.ValueOf(now)` in setBaseFields (-1 alloc/op on Insert/Update), pre-allocated result slices in `All()`/`Search()` when Limit is set (-4 allocs on limited queries), consolidated row decode pattern into `decodeIterRow` eliminating double-copy for Trackable documents
- **`dentest` helpers accept `testing.TB`** — benchmark tests can now reuse `MustOpen`/`MustOpenPostgres`
- **PostgreSQL tests always run** — removed `//go:build postgres` tag and `DEN_POSTGRES_URL` skip guard, PG is always available

## 0.1.0 — 2026-04-04

### Added

- **Core ODM** — document-oriented storage with JSONB encoding, ULID-based IDs, and automatic timestamps
- **SQLite backend** — embedded, pure Go (`modernc.org/sqlite`), JSONB storage, FTS5 full-text search
- **PostgreSQL backend** — server-based, native JSONB + GIN indexes, tsvector full-text search
- **Chainable QuerySet** — `NewQuery[T](ctx, db).Where(...).Sort(...).Limit(n).All()` with lazy evaluation
- **Range iteration** — `Iter()` returns `iter.Seq2[*T, error]` for memory-efficient streaming
- **Typed relations** — `Link[T]` for one-to-one, `[]Link[T]` for one-to-many, with cascade write/delete and eager/lazy fetch
- **Back-references** — `BackLinks[T]` finds all documents referencing a given target
- **Native aggregation** — `Avg`, `Sum`, `Min`, `Max` pushed down to SQL; `GroupBy` and `Project` for analytics
- **Full-text search** — FTS5 for SQLite, tsvector for PostgreSQL, same `Search()` API
- **Lifecycle hooks** — `BeforeInsert`, `AfterUpdate`, `Validate`, and more via interfaces on document structs
- **Change tracking** — opt-in via `TrackedBase`: `IsChanged`, `GetChanges`, `Rollback` with byte-level snapshots
- **Soft delete** — embed `SoftBase` instead of `Base`, automatic query filtering, `HardDelete` for permanent removal
- **Optimistic concurrency** — revision-based conflict detection with `ErrRevisionConflict`
- **Transactions** — `RunInTransaction` with panic-safe rollback
- **Migrations** — registry-based, each migration runs atomically in a transaction
- **Expression indexes** — `den:"index"`, `den:"unique"`, nullable unique for pointer fields
- **Struct tag validation** — optional `validate:"required,email"` tags via `go-playground/validator`, enabled with `validate.WithValidation()` option
- **Functional options** — `den.Open(backend, opts...)` pattern for extensible configuration
- **Test helpers** — `dentest.MustOpen` and `dentest.MustOpenPostgres` with automatic cleanup
