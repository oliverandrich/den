# Changelog

All notable changes to Den are documented here. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## Unreleased

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
