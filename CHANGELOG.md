# Changelog

All notable changes to Den are documented here. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## Unreleased

### Breaking Changes

- **`FindOneAndUpdate` now requires a unique match** — previously the function silently picked the first row when conditions matched more than one document. It now returns the new `ErrMultipleMatches` instead. The conditions parameter has also moved from variadic `where.Condition` to a `[]where.Condition` slice to make room for trailing `CRUDOption`s. Update call sites:

    ```go
    // before
    den.FindOneAndUpdate[Job](ctx, db, fields, where.Field("id").Eq(jobID))
    // after
    den.FindOneAndUpdate[Job](ctx, db, fields, []where.Condition{where.Field("id").Eq(jobID)})
    ```

- **`GroupByRow.Key string` → `GroupByRow.Keys []string`** and **`Backend.GroupBy(groupField string, ...)` → `Backend.GroupBy(groupFields []string, ...)`**. Internal interface contracts only — the public `QuerySet.GroupBy` API stays backward-compatible through variadic arguments. External backend implementers (none known) must adapt to the new signatures; `Keys` holds one entry per requested group field in call order.

### Added

- **`FindOneAndUpsert[T]`** — atomic find-or-create-then-update in a single transaction. Returns `(doc, inserted, err)` so callers can branch on whether the document was new. Hooks fire on exactly one path: Insert hooks on miss, Update hooks on hit. Soft-deleted matches are skipped by default; pass `IncludeSoftDeleted()` to update them in place. Concurrent upserts on the same missing row rely on a unique constraint to fail one inserter with `ErrDuplicate` — there is no internal retry.

    ```go
    user, inserted, err := den.FindOneAndUpsert[User](ctx, db,
        &User{Email: "x@y.z", LoginCount: 0},   // applied only on miss
        den.SetFields{"login_count": 5},         // applied always
        []where.Condition{where.Field("email").Eq("x@y.z")},
    )
    ```

- **`IncludeSoftDeleted()` CRUDOption** — opts lookup-style operations into considering soft-deleted documents. Honored by `FindOneAndUpdate` and `FindOneAndUpsert`.
- **Soft-delete audit fields** — `document.SoftDelete` gained optional `DeletedBy` and `DeleteReason` strings. Populate them via two new CRUDOptions:

    ```go
    den.Delete(ctx, db, doc,
        den.SoftDeleteBy("usr_42"),
        den.SoftDeleteReason("violated terms"),
    )
    ```

    Both default to empty with `omitempty`, so existing data stays compatible. Silently no-ops on the `HardDelete()` path and on types that do not embed `document.SoftDelete`.

- **`BeforeSoftDeleter` / `AfterSoftDeleter` hook interfaces** — fire only on the soft-delete path. Ordering: `BeforeDelete → BeforeSoftDelete → [write] → AfterSoftDelete → AfterDelete`. `BeforeDelete` / `AfterDelete` still fire for both soft and hard deletes, so existing hook code is unaffected. Use the soft-only pair for audit-log side effects that should not run on `HardDelete()`.
- **`CollectionMeta.HasChangeTracking`** — new bool that reports whether a registered collection implements `document.Trackable` (typically via the `document.Tracked` embed). Mirrors `HasSoftDelete` and `HasRevision` so tooling walking `Meta[T]` can detect change-tracking collections without poking at the struct itself.
- **`ErrMultipleMatches`** — returned when a single-document lookup matches more than one row.
- **`InsertMany` now accepts `...CRUDOption`** — backward-compatible signature change. Two new options ride along:
    - **`PreValidate()`** runs the full insert hook + validation chain on every document before opening the write transaction. A late-failing document fails the batch without writing anything. `BeforeInsert` / `BeforeSave` / `Validate` fire exactly once per document — the pre-pass caches the encoded bytes and the in-transaction commit only performs the Put + `AfterInsert` / `AfterSave`. Combining `PreValidate()` with `WithLinkRule(LinkWrite)` disables the caching optimization (cascade must run inside the tx), so hooks fire twice on that specific combination.
    - **`ContinueOnError()`** writes each document in its own short-lived transaction and returns an `*InsertManyError` listing per-document failures by input index. Trades cross-document atomicity for partial commit. Honors `ctx` cancellation between documents. Returns `ErrIncompatibleScope` when called inside a `*Tx`; returns `ErrIncompatibleOptions` when combined with `PreValidate`.
- **`InsertManyError`** — new struct error type carrying `[]InsertFailure{Index, Err}`. Implements `Unwrap() []error` so `errors.Is` traverses every wrapped failure.
- **`ErrIncompatibleScope` and `ErrIncompatibleOptions`** — new sentinels for option/scope mismatches.
- **Multi-key `GroupBy`** — `qs.GroupBy(fields ...string)` now accepts more than one field. Target structs declare positional slots with `den:"group_key:N"`:

    ```go
    type Stats struct {
        Category string  `den:"group_key:0"`
        Region   string  `den:"group_key:1"`
        Count    int64   `den:"count"`
        Total    float64 `den:"sum:price"`
    }
    qs.GroupBy("category", "region").Into(ctx, &stats)
    ```

    Single-field callers keep using `den:"group_key"` unchanged (treated as slot 0). Invalid tag shapes — missing slots, duplicate slots, mixed unindexed + positional tags, out-of-range slots — are caught pre-query with a clear error.

- **ORDER BY + LIMIT on `GroupBy.Into`** — grouped results are now sortable and paginatable server-side. `qs.Sort("category", den.Asc)` sorts by a group key (non-key field returns an error — use `OrderByAgg`); new `GroupByBuilder.OrderByAgg(op, field, dir)` sorts by an aggregate expression. `qs.Limit(n)` / `qs.Skip(n)` cap / offset the group rows. Combines into Top-N queries:

    ```go
    qs.Limit(5).GroupBy("category").
        OrderByAgg(den.OpCount, "", den.Desc).Into(ctx, &top)
    ```

    Previously both SQLite and PostgreSQL ignored `SortFields` / `LimitN` / `SkipN` in their `buildGroupBySQL`, forcing callers to sort and trim in Go.

### Changed

- **`QuerySet.Iter` checks `ctx.Err()` before each row** — cancellation now terminates the iteration within at most one row, regardless of how aggressively the backend's own cursor reacts to context cancellation. The seq2 error path carries the context error.
- **Per-row `ctx.Err()` check extended to the remaining drain loops** — `QuerySet.Update` (drain + write phases), `DeleteMany`, `drainIter` (shared by `All` with `WithFetchLinks`, `AllWithCount`, `Search`, `BackLinks`, `FindByIDs`), `Project`, and `forEachLinkField` (cascade write / delete / fetch-links) now all honor cancellation between rows or between link fields. Cancellation mid-bulk-Update rolls the whole batch back, matching the pre-existing all-or-nothing contract.
- **Documented `QuerySet.Update`'s fail-fast contract** — any per-row error (hook, validation, revision conflict, backend write) rolls the batch transaction back and returns `(0, err)`. Field names in `SetFields` are validated before the write transaction opens. No behavior change; docs and tests pin the existing contract.
- **`FindOneAndUpdate` / `FindOneAndUpsert` now validate `SetFields` before the transaction opens** — an unknown field name aborts the call without touching storage, mirroring the pre-tx contract `QuerySet.Update` has carried since 0.10.x. The error, position in the call graph, and semantics are otherwise unchanged.
- **Clarified that `LinkDelete` cascade is single-level** — docs previously claimed recursive cascade ("If a Door has its own links, those are also deleted"), but the code has always stopped at the immediate targets. Docs and godoc on `LinkDelete` / `cascadeDeleteLinks` / `deleteSingleLinkedValue` now match the code; no behavior change. Callers that need transitive cleanup must walk the graph themselves.
- **URL-scheme registration and lookup are now case-insensitive** in both `den.RegisterBackend` / `den.OpenURL` and `storage.Register` / `storage.OpenURL`. Both sides normalize schemes to lowercase, matching standard URL semantics: `"file"`, `"File"`, and `"FILE"` all address the same backend.
- **Latent bug fixed**: `den.RegisterBackend("SQLITE", ...)` previously stored the backend under `"SQLITE"` while `OpenURL` looked up `"sqlite"` via its pre-existing lowercasing, so the lookup silently failed. Any caller that registered with mixed-case schemes will now see their backends resolve.
- **Duplicate-registration panic** in `storage.Register` now triggers when the same scheme is registered under different casings (e.g. `"a"` then `"A"`), because both normalize to the same registry key.

### Fixed

- **Soft-delete now participates in the revision chain** — `Delete` on a document that opts into both `SoftDelete` and `UseRevision` verifies and bumps `_rev` just like `Update`. Previously the soft-delete path wrote directly without revision accounting, so a concurrent writer holding the pre-delete revision could silently clobber `DeletedAt`. Combines atomically via the same auto-wrapping write tx the update path uses; `IgnoreRevision()` opts out; `HardDelete()` is unaffected.

## 0.10.1 — 2026-04-19

### Changed

- **SQLite backend auto-creates missing parent directories** — `Open(ctx, "./data/app.db")` now `MkdirAll`s `./data` before handing the path to the driver, matching the filesystem-storage backend which has always created its root directory on construction. Fresh-checkout defaults like `sqlite:///data/app.db` now work without a manual `mkdir` step. The `:memory:` form and the `file:` URI form are left alone — those carry their own semantics (no filesystem footprint / VFS and host semantics respectively).

## 0.10.0 — 2026-04-19

### Breaking Changes

- **`storage.FilesystemStorage` moved to `storage/file`** — the filesystem backend now lives in its own sub-package, analogous to `backend/sqlite` and `backend/postgres`. This makes room for additional backends (`storage/s3`, `storage/gcs`, …) and keeps the root `storage` package trim (interface + registry only). Import-path changes:
    - `storage.FilesystemStorage` → `file.Storage`
    - `storage.NewFilesystemStorage(root, urlPrefix)` → `file.New(root, urlPrefix)`
    - Import `github.com/oliverandrich/den/storage/file` instead of (or in addition to) `github.com/oliverandrich/den/storage`.

### Added

- **`storage.OpenURL(dsn, urlPrefix)` + scheme registry** — a DSN-based factory that dispatches to the backend registered for the scheme. Backends register themselves via `storage.Register("scheme", opener)` from an `init()`, matching the pattern Den already uses for database backends. The filesystem backend side-effect-registers `file://`:

    ```go
    import (
        "github.com/oliverandrich/den/storage"
        _ "github.com/oliverandrich/den/storage/file" // registers file://
    )

    fs, err := storage.OpenURL("file:///uploads", "/media")
    ```

    The `file://` DSN follows the same SQLAlchemy/JDBC-style convention as `sqlite://` for consistency: **three slashes for a relative path, four slashes for an absolute path** (the leading slash of the path is stripped on parse, which lets standard URL libraries treat the path component uniformly with the authority empty). Examples:

    - `file:///data/media` → relative `data/media`
    - `file:////var/media` → absolute `/var/media`

    Direct construction via `file.New(...)` still works and takes the filesystem path literally. The registry enables config-driven setups (such as Burrow's `--storage-dsn` flag) to pick the backend at runtime without code changes when future backends land.

## 0.9.1 — 2026-04-19

### Added

- **`(*FilesystemStorage).URLPrefix() string`** — returns the HTTP path prefix the storage serves its files under. HTTP-layer packages (`burrow/contrib/uploads`) type-assert on a `URLPrefix() string` interface to mount their serving handler on the same route the `URL` method produces. Remote-URL backends (S3, GCS) intentionally do not implement this — the absent method is the signal to skip local serving.

## 0.9.0 — 2026-04-19

### Added

- **`document.Attachment` embed** — a reusable file-reference field for documents. Carries `StoragePath`, `Mime`, `Size`, and `SHA256`, all validated via struct tags. Embed it to turn a document INTO a file (`type Media struct { document.Base; document.Attachment; ... }`) or add it as named fields to have ONE document point at MULTIPLE files (`type Product struct { document.Base; Hero, Thumbnail document.Attachment }`). `IsZero()` distinguishes "no file attached yet" from "file present".
- **`den.Storage` interface** — `Store(ctx, r, ext, mime) (Attachment, error)`, `Open(ctx, Attachment) (io.ReadCloser, error)`, `Delete(ctx, Attachment) error`, `URL(Attachment) string`. Installed on the DB via `den.WithStorage(...)`. Reachable at runtime via `db.Storage()`. One Storage per DB — application code that owns the upload flow (web handlers, CLI importers) calls it directly.
- **`den/storage.FilesystemStorage`** — reference Storage implementation. Content-addresses paths to `YYYY/MM/<sha256-prefix>.<ext>` so identical uploads dedupe on both disk and the unique StoragePath index. `os.Root` guards every open/remove against path traversal, idempotent Delete tolerates missing paths, and zero-byte uploads are refused at the boundary. Used to live inline in `warren`; hoisted here so every Den-using project gets it.
- **Hard-delete cascade for attachments** — `den.Delete(..., den.HardDelete())` on a document that contains one or more `document.Attachment` fields now asks the configured Storage to remove the bytes. Walked via reflection into embedded structs and pointer-to-struct fields; zero Attachments are skipped. Best-effort: storage failures are logged via `slog` but do not fail the database delete — orphan bytes are recoverable via an offline sweep, while the reverse (surviving DB references to missing bytes) would break the public site. No `Storage` installed + non-zero attachments → warning log, delete proceeds.
- **`zizmor` workflow audit in CI** — new `zizmor` job in `.github/workflows/ci.yml` runs the [zizmor](https://docs.zizmor.sh/) static analyzer against all workflow files on every PR and push. A `.github/zizmor.yml` config documents the one accepted risk (the `workflow_run` trigger in `release.yml`, gated on branch-prefix + CI success).

### Changed

- **CI and release workflows hardened** — all `actions/*` and `golangci/*` uses are now pinned to commit SHAs with version comments (the blanket policy zizmor enforces). `persist-credentials: false` on every `actions/checkout`. `actions/setup-go` runs with `cache: false` to prevent cache-poisoning on tag pushes. `release.yml` routes `github.event.workflow_run.head_branch` through a `VERSION` env var instead of direct `${{ … }}` interpolation inside `run:` blocks, closing the template-injection vector. The manual `actions/cache` steps were removed; setup-go's built-in cache handling would have re-introduced the poisoning concern without offering meaningful speedup.

## 0.8.0 — 2026-04-18

### Breaking Changes

- **`Tx*` CRUD functions unified into a sealed `Scope` interface** — `Insert`, `InsertMany`, `Update`, `Delete`, `DeleteMany`, `FindByID`, `FindByIDs`, `FindOneAndUpdate`, `Refresh`, `FetchLink`, `FetchAllLinks`, `BackLinks` now accept a `den.Scope` parameter satisfied by both `*DB` and `*Tx`. The `TxInsert` / `TxUpdate` / `TxDelete` / `TxFindByID` variants are removed. Migration: replace `den.TxInsert(tx, doc)` with `den.Insert(ctx, tx, doc)` — `ctx` is already in scope from the enclosing `RunInTransaction(ctx, db, …)` closure. `Scope` is sealed (unexported methods) so only `*DB` and `*Tx` can satisfy it; backend authors are unaffected. `InsertMany` / `DeleteMany` / `FindOneAndUpdate` keep their auto-tx behavior when the scope is `*DB` and run inline when the scope is `*Tx`.
- **`Tx` no longer stores `context.Context`.** The previously-implicit `tx.ctx` is gone; every tx-scoped entry point takes `ctx` explicitly, matching the precedent set by `QuerySet.All(ctx)`. Tx-scope-only operations also drop the now-redundant `Tx` prefix — the `*Tx` parameter already enforces the transaction-scope constraint. Migration:
    - `den.TxLockByID(tx, id, opts…)` → `den.LockByID(ctx, tx, id, opts…)`
    - `den.TxRawGet(tx, col, id)` and `den.TxRawPut(tx, col, id, data)` are removed entirely — they were a public escape hatch that invited misuse alongside `Insert`/`Update`. Infrastructure code that genuinely needs raw bytes (the migration log) now uses the new `(t *Tx) Transaction() Transaction` accessor: `tx.Transaction().Get(ctx, col, id)` / `tx.Transaction().Put(ctx, col, id, data)`. The accessor is documented as low-level and not intended for application code
    - `den.TxAdvisoryLock(tx, key)` → `den.AdvisoryLock(ctx, tx, key)`
    - `TxQuerySet.All()` → `TxQuerySet.All(ctx)`; same for `First`
- **`NewTxQuery` / `TxQuerySet` removed — `NewQuery` now takes `Scope`** — the follow-up step of the Scope unification. `NewQuery[T](scope Scope, ...)` accepts `*DB` and `*Tx` just like the CRUD helpers do; the separate transaction-scoped builder goes away. `ForUpdate(opts ...LockOption)` becomes a chain method on the unified `QuerySet[T]`. Calling `ForUpdate` on a `*DB`-bound QuerySet is accepted syntactically but terminal methods return the new sentinel `den.ErrLockRequiresTransaction`. Migration: `den.NewTxQuery[T](tx, conds...).ForUpdate().All(ctx)` → `den.NewQuery[T](tx, conds...).ForUpdate().All(ctx)`; `TxQuerySet[T]` references → `QuerySet[T]`.
- **`den.ErrLockRequiresTransaction`** added as the sentinel surfaced when `QuerySet.ForUpdate` is set but the scope is a `*DB`.
- **`den.Rollback` renamed to `den.Revert`** — the change-tracking helper that restores a document to its snapshot state has nothing to do with transactions; the old name collided with `tx.Rollback()` every time both appeared in the same file. Single-symbol rename, semantics unchanged. Migration: `den.Rollback(db, doc)` → `den.Revert(db, doc)`.
- **`document.SoftBase` / `TrackedBase` / `TrackedSoftBase` collapsed into composable embeds** — the four named base types were a 2² matrix of two orthogonal features (soft delete × change tracking). The matrix is gone; `document.Base` stays required, and `document.SoftDelete` and `document.Tracked` are now independent composable embeds. Compose freely: `struct { document.Base; document.SoftDelete; document.Tracked; ... }`. Migration:
    - `document.SoftBase` → `document.Base` + `document.SoftDelete`
    - `document.TrackedBase` → `document.Base` + `document.Tracked`
    - `document.TrackedSoftBase` → `document.Base` + `document.SoftDelete` + `document.Tracked`
    JSON wire format is unchanged (only struct layout changes). Internal detection is structural (`_deleted_at` field / `Trackable` interface), so any type that matches the shape participates — not just these named embeds.
- **`CollectionMeta.HasSoftBase` renamed to `HasSoftDelete`** — consistency with the new embed name. Custom backend implementations that read this field must update.

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
- **PostgreSQL simple `Eq` predicates now use containment so the GIN index can serve them** — `where.Field("status").Eq("published")` previously emitted `jsonb_extract_path(data, 'status') = $1::jsonb`, a functional expression the `GIN(data jsonb_path_ops)` index cannot satisfy, so Postgres fell back to a sequential scan. The builder now emits `data @> $1::jsonb` with a `{"status":"published"}` parameter when the LHS is a top-level field and the RHS is a scalar (`string`, `bool`, integer, float). The GIN index is used directly; `EXPLAIN` shows `Bitmap Index Scan` instead of `Seq Scan`. Nested paths, non-scalar values (slices, maps), `nil`, and `FieldRef` comparisons continue to use the extract form, because containment would either have different semantics (subset vs equality) or cannot build a safe top-level JSONB literal. `Ne`/`Gt`/`Lt`/`Gte`/`Lte` are unchanged — `jsonb_path_ops` cannot satisfy range predicates either way
- **`QuerySet.All(ctx)` with `WithFetchLinks()` batches link resolution instead of per-row Get** — the previous implementation reused `.Iter()`, which called `Get` per linked document and per row. For N parents with one link each that was N round-trips on PostgreSQL. `.All()` now drains the iterator first, then resolves each link field in one `WHERE _id IN (…)` query per target type per nesting level, deduplicating IDs so a hot target shared across many parents is fetched once. Parents referencing the same target id now share the decoded pointer (observable via `==`). `WithFetchLinks` on streaming `.Iter()` is unchanged and still resolves per row so iteration stays streaming. On the benchmark with 20 parents + one shared author `WithFetchLinks` drops from ~1.6 ms to ~600 µs on PostgreSQL (~2.7× faster); on SQLite from ~107 µs to ~73 µs. `QuerySet.AllWithCount` and `QuerySet.Search` use the same batched resolver
- **`WithNestingDepth(n)` now resolves links recursively on loaded targets** — the previous per-row resolver passed the depth value around but never descended: a `WithNestingDepth(2)` query against `Root → Mid → Leaf` only loaded `Mid`. The batched `.All()` / `.AllWithCount()` / `.Search()` path now runs one batched query per depth level, so `Root.Mid.Value.Leaf` is now populated as documented. Streaming `.Iter()` still only resolves the direct level

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
