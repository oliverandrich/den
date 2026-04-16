# API Reference

Complete reference of all public functions in the `den` package, organized by category.

Module: `github.com/oliverandrich/den`

---

## Database

| Function | Signature | Description |
|---|---|---|
| `OpenURL` | `OpenURL(dsn string, opts ...Option) (*DB, error)` | Open a database using a URL-style DSN (requires backend import) |
| `Register` | `Register(ctx context.Context, db *DB, docs ...any) error` | Register document types; creates collections and indexes |
| `db.Close` | `(db *DB) Close() error` | Close the database connection |
| `db.Ping` | `(db *DB) Ping(ctx context.Context) error` | Healthcheck; delegates to backend |

---

## CRUD

### Insert

| Function | Signature | Description |
|---|---|---|
| `Insert[T]` | `Insert[T](ctx context.Context, db *DB, doc *T, opts ...CRUDOption) error` | Insert a single document. ID is auto-generated (ULID) if empty |
| `InsertMany[T]` | `InsertMany[T](ctx context.Context, db *DB, docs []*T) error` | Insert multiple documents in a single batch |

### Read

| Function | Signature | Description |
|---|---|---|
| `FindByID[T]` | `FindByID[T](ctx context.Context, db *DB, id string) (*T, error)` | Find a document by its ID (direct key lookup) |
| `FindByIDs[T]` | `FindByIDs[T](ctx context.Context, db *DB, ids []string) ([]*T, error)` | Find multiple documents by their IDs |

### Update

| Function | Signature | Description |
|---|---|---|
| `Update[T]` | `Update[T](ctx context.Context, db *DB, doc *T, opts ...CRUDOption) error` | Update an existing document (full document write) |
| `FindOneAndUpdate[T]` | `FindOneAndUpdate[T](ctx context.Context, db *DB, fields SetFields, conditions ...where.Condition) (*T, error)` | Atomically find the first matching document, apply field updates, and return the modified document |
| `Refresh[T]` | `Refresh[T](ctx context.Context, db *DB, doc *T) error` | Re-read the document from storage, replacing all field values |

### Delete

| Function | Signature | Description |
|---|---|---|
| `Delete[T]` | `Delete[T](ctx context.Context, db *DB, doc *T, opts ...CRUDOption) error` | Delete a document. Soft-deletes if the document embeds `SoftBase` |
| `DeleteMany[T]` | `DeleteMany[T](ctx context.Context, db *DB, conditions []where.Condition, opts ...CRUDOption) (int64, error)` | Delete all documents matching the given conditions |
| `HardDelete[T]` | `HardDelete[T](ctx context.Context, db *DB, doc *T) error` | Permanently remove a document, bypassing soft-delete |

---

## Query

### Creating a Query

```go
q := den.NewQuery[T](ctx, db, conditions...)
```

| Function | Signature | Description |
|---|---|---|
| `NewQuery[T]` | `NewQuery[T](ctx context.Context, db *DB, conditions ...where.Condition) QuerySet[T]` | Create a new chainable query for type T |

### Chainable Methods

All chainable methods return `QuerySet[T]` and can be composed in any order.

| Method | Signature | Description |
|---|---|---|
| `Where` | `Where(conditions ...where.Condition) QuerySet[T]` | Add additional filter conditions |
| `Sort` | `Sort(field string, dir SortDirection) QuerySet[T]` | Sort results by field (`den.Asc` or `den.Desc`) |
| `Limit` | `Limit(n int) QuerySet[T]` | Limit the number of results |
| `Skip` | `Skip(n int) QuerySet[T]` | Skip the first n results (offset-based pagination) |
| `After` | `After(id string) QuerySet[T]` | Cursor-based pagination: fetch results after this ID |
| `Before` | `Before(id string) QuerySet[T]` | Cursor-based pagination: fetch results before this ID |
| `WithFetchLinks` | `WithFetchLinks() QuerySet[T]` | Eagerly resolve all `Link[T]` fields on results |
| `WithNestingDepth` | `WithNestingDepth(n int) QuerySet[T]` | Override max link-fetching depth for this query |
| `IncludeDeleted` | `IncludeDeleted() QuerySet[T]` | Include soft-deleted documents in results |

### Terminal Methods

Terminal methods execute the query and return results.

| Method | Signature | Description |
|---|---|---|
| `All` | `All() ([]*T, error)` | Execute query, return all matching documents |
| `First` | `First() (*T, error)` | Execute query, return the first matching document |
| `Count` | `Count() (int64, error)` | Count matching documents |
| `Exists` | `Exists() (bool, error)` | Check whether at least one matching document exists |
| `AllWithCount` | `AllWithCount() ([]*T, int64, error)` | Return matching documents and total count (for pagination) |
| `Iter` | `Iter() iter.Seq2[*T, error]` | Return a lazy iterator for streaming results with `range` |
| `Update` | `Update(fields SetFields) (int64, error)` | Bulk update all matching documents, return count of updated |
| `Search` | `Search(query string) ([]*T, error)` | Full-text search using FTS5 (SQLite) or tsvector (PostgreSQL) |

---

## Aggregation

Aggregation methods are chained onto a `QuerySet[T]`.

### Scalar Aggregations

| Method | Signature | Description |
|---|---|---|
| `Avg` | `Avg(field string) (float64, error)` | Average of a numeric field across matching documents |
| `Sum` | `Sum(field string) (float64, error)` | Sum of a numeric field across matching documents |
| `Min` | `Min(field string) (float64, error)` | Minimum value of a field across matching documents |
| `Max` | `Max(field string) (float64, error)` | Maximum value of a field across matching documents |

### Grouped Aggregations

| Method | Signature | Description |
|---|---|---|
| `GroupBy` | `GroupBy(field string) *GroupByBuilder[T]` | Group results by a field |
| `Into` | `Into(dest any) error` | Execute grouped aggregation into a target slice of structs |
| `Project` | `Project(dest any) error` | Project query results into a struct with a subset of fields |

```go
// GroupBy example
type Stats struct {
    Category string  `den:"group_key"`
    AvgPrice float64 `den:"avg:price"`
    Count    int64   `den:"count"`
}

err := den.NewQuery[Product](ctx, db).GroupBy("category.name").Into(&results)
```

---

## Relations

| Function | Signature | Description |
|---|---|---|
| `NewLink[T]` | `NewLink[T any](doc *T) Link[T]` | Create a Link from an existing document, extracting its ID |
| `FetchLink[T]` | `FetchLink[T](ctx context.Context, db *DB, doc *T, field string) error` | Fetch and resolve a single link field on a document |
| `FetchAllLinks[T]` | `FetchAllLinks[T](ctx context.Context, db *DB, doc *T) error` | Fetch and resolve all link fields on a document |
| `BackLinks[T]` | `BackLinks[T](ctx context.Context, db *DB, linkField string, targetID string) ([]*T, error)` | Find all documents of type T that reference the given target ID via the named link field |
| `WithLinkRule` | `WithLinkRule(rule LinkRule) CRUDOption` | Set cascade behavior for insert/update/delete of linked documents |

### Link Rules

| Rule | Value | Description |
|---|---|---|
| `LinkIgnore` | `0` | No cascading -- only the root document is written/deleted |
| `LinkWrite` | `1` | Cascade writes to all linked documents (insert new, update existing) |
| `LinkDelete` | `2` | Cascade deletion to all linked documents |

---

## Change Tracking

Requires embedding `document.TrackedBase` (or `document.TrackedSoftBase`) instead of `document.Base`.

| Function | Signature | Description |
|---|---|---|
| `IsChanged[T]` | `IsChanged[T](db *DB, doc *T) (bool, error)` | Check whether the document has been modified since last load/save |
| `GetChanges[T]` | `GetChanges[T](db *DB, doc *T) (map[string]FieldChange, error)` | Get a map of changed fields with before/after values |
| `Rollback` | `Rollback(db *DB, doc any) error` | Restore the document to its last-saved state. Returns `ErrNoSnapshot` if no snapshot exists |

---

## Transactions

| Function | Signature | Description |
|---|---|---|
| `RunInTransaction` | `RunInTransaction(ctx context.Context, db *DB, fn func(tx *Tx) error) error` | Execute a function within a transaction. Commits on nil return, rolls back on error |
| `TxFindByID[T]` | `TxFindByID[T](tx *Tx, id string) (*T, error)` | Find a document by ID within a transaction |
| `TxLockByID[T]` | `TxLockByID[T](tx *Tx, id string) (*T, error)` | Find a document by ID and acquire a row-level lock (`SELECT ... FOR UPDATE` on PostgreSQL; no-op on SQLite). Held until the transaction commits or rolls back |
| `TxInsert[T]` | `TxInsert[T](tx *Tx, doc *T) error` | Insert a document within a transaction |
| `TxUpdate` | `TxUpdate(tx *Tx, doc any) error` | Update a document within a transaction |
| `TxDelete[T]` | `TxDelete[T](tx *Tx, doc *T) error` | Delete a document within a transaction |
| `TxGet` | `TxGet(tx *Tx, collection string, id string) ([]byte, error)` | Get raw document bytes by collection and ID within a transaction |
| `TxPut` | `TxPut(tx *Tx, collection string, id string, data []byte) error` | Put raw document bytes by collection and ID within a transaction |

> **Note:** All standard CRUD operations have `Tx` variants for use inside transactions.

---

## Metadata

| Function | Signature | Description |
|---|---|---|
| `Meta[T]` | `Meta[T](db *DB) (CollectionMeta, error)` | Get metadata for a registered collection (fields, indexes, links, settings) |
| `Collections` | `Collections(db *DB) []string` | List all registered collection names |

---

## Index Lifecycle

| Function | Signature | Description |
|---|---|---|
| `DropStaleIndexes` | `DropStaleIndexes(ctx, db *DB, opts ...DropStaleOption) (DropStaleResult, error)` | Drop indexes previously created by `Register()` that no longer correspond to any `IndexDefinition`. Managed indexes (GIN, FTS) are never touched |
| `DryRun` | `DryRun() DropStaleOption` | Option for `DropStaleIndexes`; reports the plan without mutating the database |

`DropStaleResult` contains two slices:

- `Dropped []StaleIndex` — indexes that were (or would be, under DryRun) removed
- `Kept []StaleIndex` — recorded indexes that are still referenced by a current `IndexDefinition`

`StaleIndex` has fields `Collection`, `Name`, `Fields []string`, `Unique bool`.

---

## Migrations

Located in the `migrate` sub-package (`github.com/oliverandrich/den/migrate`).

| Function | Signature | Description |
|---|---|---|
| `NewRegistry` | `NewRegistry() *Registry` | Create a new migration registry |
| `Register` | `(r *Registry) Register(version string, m Migration)` | Register a migration with a version string |
| `Up` | `(r *Registry) Up(ctx context.Context, db *den.DB) error` | Run all pending forward migrations |
| `UpOne` | `(r *Registry) UpOne(ctx context.Context, db *den.DB) error` | Run one forward migration |
| `Down` | `(r *Registry) Down(ctx context.Context, db *den.DB) error` | Roll back all migrations |
| `DownOne` | `(r *Registry) DownOne(ctx context.Context, db *den.DB) error` | Roll back one migration |

---

## Testing Helpers

Located in the `dentest` sub-package (`github.com/oliverandrich/den/dentest`).

| Function | Signature | Description |
|---|---|---|
| `MustOpen` | `MustOpen(t testing.TB, docs ...any) *den.DB` | Open a file-backed SQLite database in a temp directory; auto-registers docs and cleans up after test |
| `MustOpenPostgres` | `MustOpenPostgres(t testing.TB, connStr string, docs ...any) *den.DB` | Open a PostgreSQL database for testing; auto-registers docs |

---

## Key Types

| Type | Description |
|---|---|
| `DB` | Database handle; holds the backend and collection registry |
| `Tx` | Transaction handle; wraps a backend transaction |
| `Link[T]` | Generic reference to a document in another collection; stores ID, optionally holds resolved Value |
| `SetFields` | `map[string]any` used for partial updates via `FindOneAndUpdate` and bulk `Update` |
| `Settings` | Document-level settings (collection name, revision, nesting depth, indexes) |
| `QuerySet[T]` | Chainable, lazy query builder |
| `SortDirection` | Sort direction: `den.Asc` or `den.Desc` |
| `LinkRule` | Cascade behavior for link operations |
| `Option` | Functional option for `Open`/`OpenURL` |
| `CRUDOption` | Functional option for write operations (e.g., `WithLinkRule`) |
| `FieldChange` | Represents a changed field with `Before` and `After` values |
| `CollectionMeta` | Metadata about a collection: fields, indexes, links, settings |
| `IndexDefinition` | Index specification: name, fields, unique flag |
