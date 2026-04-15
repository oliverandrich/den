# Den – ODM for Go

## Project Summary

**Den** is an Object-Document Mapper (ODM) for Go with two storage backends. It provides a MongoDB/Beanie-style document model using native Go structs, with support for relations, lifecycle hooks, transactions, and a fluent query builder.

Den ships with two storage backends:

* **SQLite** – Embedded, stores documents as JSONB. Leverages SQLite's mature query planner, expression indexes, and FTS5 for full-text search. Pure Go via `modernc.org/sqlite`.
* **PostgreSQL** – Server-based, full JSONB with GIN indexes. For applications that need replication, scaling, or advanced query capabilities including native full-text search via `tsvector`.

Both backends expose the identical Den API. Application code is backend-agnostic — switch from embedded SQLite to PostgreSQL by changing one line.

Den is part of the **Burrow** ecosystem. Its name follows the gopher/burrow metaphor: a *den* is where a gopher stores and retrieves things.

### Design Goals

1. **Beanie-like ergonomics** – Struct-based documents, fluent queries, relations via `Link[T]`, lifecycle hooks. Developers familiar with [Beanie ODM](https://beanie-odm.dev) should feel at home.
2. **Backend-agnostic** – One API, two storage engines. Choose the right backend for your workload without changing application code.
3. **Embeddable by default** – The SQLite backend requires no external process. Opens a file, ready to go. Fits Burrow's philosophy of self-contained deployments.
4. **Transactional** – Full transaction support across both backends via `RunInTransaction`.
5. **Pure Go for the embedded backend** – SQLite via `modernc.org/sqlite` requires no CGO. Cross-compilation friendly.
6. **Pluggable for Burrow apps** – Each Burrow app registers its own document types, similar to how Django apps register models.

### Non-Goals

* Den does **not** implement MongoDB's query language (MQL) or multi-stage aggregation pipelines.
* Den does **not** aim to support distributed consensus or multi-node replication (use PostgreSQL's native replication for that).
* Den is **not** a relational database. It does not support SQL or arbitrary JOINs.
* **No MySQL backend** – MySQL cannot directly index JSON columns; it requires generated virtual columns as a workaround. This adds significant complexity to the SQL generator for marginal benefit. PostgreSQL is the superior choice for server-based JSON/document workloads.
* **No Views** – MongoDB Views are server-side materialized aggregations maintained by a background process. An embedded DB has no such process. Use application-level caching or aggregation queries instead.
* **No Time Series collections** – MongoDB 5.0+ time series with automatic bucketing and compaction is deeply integrated into MongoDB's storage engine. Disproportionate effort for an ODM.
* **No Multi-Model / UnionDoc** – Storing different document types in one collection with a discriminator field is technically possible, but yields poor Go ergonomics (requires `any` returns and type assertions). Use separate collections per type instead.
* **No Class Inheritance polymorphism** – Beanie's `is_root = True` relies on Python's class hierarchy. Go uses composition over inheritance. Den embraces this: use interfaces and struct embedding rather than polymorphic collections.
* **No Lazy Parsing** – Beanie defers Pydantic field validation until access. Go's JSON decoding is already fast and statically typed, making this optimization unnecessary.

***

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────┐
│                       User Application                        │
├──────────────────────────────────────────────────────────────┤
│  Layer 5: Registration & Init                                 │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │  den.OpenURL() / den.Register() / den.WithSQLite() / ...   │ │
│  │  Auto-index creation, schema validation                  │ │
│  └──────────────────────────────────────────────────────────┘ │
├──────────────────────────────────────────────────────────────┤
│  Layer 4: Lifecycle & Middleware                               │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │  BeforeInsert / AfterInsert / BeforeDelete / ...         │ │
│  │  Validation hooks, event-based actions                   │ │
│  │  Revision control (optimistic concurrency)               │ │
│  │  Soft delete (DeletedAt filtering)                       │ │
│  │  State management (change tracking, rollback)            │ │
│  │  Query result caching (LRU + TTL)                        │ │
│  └──────────────────────────────────────────────────────────┘ │
├──────────────────────────────────────────────────────────────┤
│  Layer 3: Query Builder, Aggregation & Relations              │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │  Fluent query API: where.Field("x").Eq(y)                │ │
│  │  Aggregation: Avg, Sum, Min, Max, Count, GroupBy         │ │
│  │  Link[T] resolution, fetch_links, link_rules             │ │
│  │  Sort, Limit, Skip, Projection                           │ │
│  └──────────────────────────────────────────────────────────┘ │
├──────────────────────────────────────────────────────────────┤
│  Layer 2: Backend Interface                                   │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │  type Backend interface {                                │ │
│  │      Get, Put, Delete, Query, EnsureIndex, Begin, Close  │ │
│  │  }                                                       │ │
│  └──────────────────────────────────────────────────────────┘ │
├──────────────────────┬───────────────────────────────────────┤
│  SQLite              │  PostgreSQL                            │
│  Backend             │  Backend                               │
│ ┌──────────────────┐ │ ┌───────────────────────────────────┐ │
│ │ JSON/JSONB       │ │ │ JSONB                             │ │
│ │ Expression       │ │ │ GIN + Expression                  │ │
│ │ Indexes          │ │ │ Indexes                           │ │
│ │ SQL Query        │ │ │ SQL Query Planner                 │ │
│ │ Planner          │ │ │ + Containment Ops                 │ │
│ │ FTS5             │ │ │ tsvector / tsquery                │ │
│ └──────────────────┘ │ └───────────────────────────────────┘ │
│  embedded            │  server (net/TCP)                      │
│  pure Go             │  requires running PG                   │
│  modernc/sqlite      │  pgx driver                            │
└──────────────────────┴───────────────────────────────────────┘
```

***

## Package Structure

```
den/
├── den.go                  # Public API: Open, Close, Register
├── backend.go              # Backend, ReadWriter, Transaction interfaces
├── collection.go           # Collection registry, metadata
├── crud.go                 # Insert, Update, Delete, FindByID, etc.
├── queryset.go             # Chainable QuerySet[T] query builder
├── iter.go                 # Iter() — iter.Seq2[*T, error] for range loops
├── aggregate.go            # Aggregation: Avg, Sum, Min, Max, GroupBy, Project
├── link.go                 # Link[T] type, relation handling, cascade
├── backlinks.go            # BackLinks reverse query
├── search.go               # FTS integration (FTSProvider interface)
├── track.go                # Change tracking: IsChanged, GetChanges, Rollback
├── soft_delete.go          # Soft delete logic, HardDelete
├── hooks.go                # Lifecycle hook interfaces
├── revision.go             # Optimistic concurrency (revision IDs)
├── settings.go             # Settings struct, DenSettable interface
├── errors.go               # Typed errors (ErrNotFound, ErrRevisionConflict, ...)
├── tx.go                   # Transaction wrapper (RunInTransaction, Tx*)
├── where/                  # Query condition builders
│   └── where.go            # Field("x").Eq(y), Lt, Gt, In, ...
├── document/               # Base document types
│   ├── base.go             # Base, TrackedBase, Trackable interface
│   └── soft_base.go        # SoftBase, TrackedSoftBase
├── backend/
│   ├── sqlite/             # SQLite backend (embedded, pure Go)
│   │   ├── sqlite.go       # Backend implementation, prepared stmt cache
│   │   ├── transaction.go  # Transaction implementation
│   │   ├── schema.go       # Table creation, expression indexes
│   │   ├── sql.go          # Query-to-SQL translation (SQLite dialect)
│   │   ├── encoding.go     # JSON encoding helpers
│   │   └── fts.go          # FTS5 virtual table management
│   └── postgres/           # PostgreSQL backend (server-based)
│       ├── postgres.go     # Backend implementation, SQL string cache
│       ├── transaction.go  # Transaction implementation
│       ├── schema.go       # Table creation, GIN + expression indexes
│       ├── sql.go          # Query-to-SQL translation (PG dialect)
│       ├── encoding.go     # JSON encoding helpers
│       └── fts.go          # tsvector column and GIN FTS index management
├── migrate/                # Migration framework
│   └── migrate.go          # Registry, runner (forward/backward)
├── internal/
│   └── reflect.go          # Struct reflection, tag parsing, field access
└── dentest/                # Test helpers for consumers
    └── dentest.go          # File-backed SQLite factory for tests
```

***

## Layer 2: Backend Interface

The backend interface is the central abstraction that enables Den's multi-backend architecture. All storage operations flow through this interface. Layers 3–5 are completely backend-agnostic.

### Interface Definition

```go
package den

// ReadWriter is the shared interface between Backend and Transaction.
// All read/write operations flow through this abstraction, allowing
// CRUD code to work identically inside and outside transactions.
type ReadWriter interface {
    Get(ctx context.Context, collection, id string) ([]byte, error)
    Put(ctx context.Context, collection, id string, data []byte) error
    Delete(ctx context.Context, collection, id string) error
    Query(ctx context.Context, collection string, q *Query) (Iterator, error)
    Count(ctx context.Context, collection string, q *Query) (int64, error)
    Exists(ctx context.Context, collection string, q *Query) (bool, error)
    Aggregate(ctx context.Context, collection string, op AggregateOp, field string, q *Query) (*float64, error)
}

// Backend defines the contract that all storage engines must implement.
type Backend interface {
    ReadWriter

    // Index management
    EnsureIndex(ctx context.Context, collection string, idx IndexDefinition) error
    DropIndex(ctx context.Context, collection string, name string) error

    // Collection management
    EnsureCollection(ctx context.Context, name string, meta CollectionMeta) error
    DropCollection(ctx context.Context, name string) error

    // Transactions
    Begin(ctx context.Context, writable bool) (Transaction, error)

    // Encoding — backend provides its own JSON encoder
    Encoder() Encoder

    // Lifecycle
    Ping(ctx context.Context) error
    Close() error
}

// Transaction extends ReadWriter with Commit/Rollback.
type Transaction interface {
    ReadWriter
    Commit() error
    Rollback() error
}

// Iterator provides sequential access to query results.
// IMPORTANT: Bytes() may return a buffer reused on the next Next() call.
// Consumers must copy the bytes before advancing the iterator.
type Iterator interface {
    Next() bool
    Bytes() []byte
    ID() string
    Err() error
    Close() error
}

// AggregateOp defines supported aggregation operations.
type AggregateOp string

const (
    OpSum AggregateOp = "SUM"
    OpAvg AggregateOp = "AVG"
    OpMin AggregateOp = "MIN"
    OpMax AggregateOp = "MAX"
)
```

### Opening a Database with a Backend

```go
import (
    "your/module/den"
    "your/module/den/backend/sqlite"
    "your/module/den/backend/postgres"
)

// SQLite: embedded, JSONB, mature query planner
db, err := den.Open(sqlite.Open("./data/myapp.db"))

// PostgreSQL: server-based, GIN indexes, scalable
db, err := den.Open(postgres.Open("postgres://user:pass@localhost/mydb"))
```

After `den.Open()`, the API is identical regardless of backend. Application code never imports a backend package directly (except at the initialization site).

***

## Backend Implementations

### SQLite Backend (embedded, pure Go)

SQLite via `modernc.org/sqlite` (pure Go, no CGO) with JSONB support (requires SQLite >= 3.45.0).

**Characteristics:**

* Pure Go (via modernc.org/sqlite transpilation)
* Mature query planner with cost-based optimization
* WAL mode for concurrent readers
* Single writer (but writes are fast for typical workloads)
* Litestream-compatible for streaming backups
* Built-in FTS5 for full-text search

**Document storage:** Each Den collection becomes a SQLite table:

```sql
CREATE TABLE IF NOT EXISTS products (
    id   TEXT PRIMARY KEY,
    data BLOB NOT NULL      -- JSONB-encoded document (all fields including timestamps)
);
```

Documents are stored as JSONB BLOBs using SQLite's `jsonb()` function. All document fields — including `_id`, `_created_at`, `_updated_at`, `_deleted_at`, and `_rev` — live inside the `data` column. The `id` column mirrors `_id` for efficient primary key lookups.

**Query translation:** Den queries are translated to SQL with `json_extract()`:

```go
// Den:
den.NewQuery[Product](ctx, db,
    where.Field("price").Gt(10),
    where.Field("category.name").Eq("Chocolate"),
).Sort("price", den.Asc).Limit(20).All()

// Generated SQL:
SELECT id, json(data) FROM "products"
WHERE json_extract(data, '$.price') > ?
  AND json_extract(data, '$.category.name') = ?
ORDER BY json_extract(data, '$.price') ASC
LIMIT 20
```

**Indexes:** Expression indexes on JSON paths:

```sql
-- Generated from: den:"index" struct tag on Price field
CREATE INDEX IF NOT EXISTS idx_products_price
    ON products(json_extract(data, '$.price'));

-- Generated from: den:"unique" struct tag on SKU field
CREATE UNIQUE INDEX IF NOT EXISTS idx_products_sku
    ON products(json_extract(data, '$.sku'));
```

**Aggregation:** Delegated to SQLite's native SQL aggregation — significantly faster than in-memory accumulation for large collections:

```sql
-- den.NewQuery[Product](ctx, db).GroupBy("category.name").Into(&results)
SELECT json_extract(data, '$.category.name') AS group_key,
       AVG(json_extract(data, '$.price')) AS avg_price,
       COUNT(*) AS total_count
FROM products
WHERE _deleted_at IS NULL
GROUP BY json_extract(data, '$.category.name')
```

**Best for:** Balanced read/write workloads, complex queries, applications that benefit from SQLite's query planner, streaming backups via Litestream, single-binary deployments, use cases where a mature embedded SQL engine is preferred.

### PostgreSQL Backend (server-based)

PostgreSQL via `github.com/jackc/pgx/v5` with full JSONB and GIN index support.

**Characteristics:**

* Server-based (requires a running PostgreSQL instance)
* Most powerful query planner of both backends
* GIN indexes for containment queries (`@>` operator)
* Native JSONB with in-place updates
* Concurrent readers and writers
* Built-in replication, streaming backups, point-in-time recovery
* Native full-text search via `tsvector`/`tsquery`

**Document storage:** Same table structure as SQLite, but with native JSONB type:

```sql
CREATE TABLE IF NOT EXISTS products (
    id   TEXT PRIMARY KEY,
    data JSONB NOT NULL      -- all document fields including timestamps
);
```

**Query translation:** Uses PostgreSQL's native JSONB operators:

```go
// Den:
den.NewQuery[Product](ctx, db,
    where.Field("price").Gt(10),
    where.Field("category.name").Eq("Chocolate"),
).All()

// Generated SQL (PostgreSQL dialect):
SELECT id, data::text FROM "products"
WHERE (data->>'price')::float > $1
  AND data->'category'->>'name' = $2
```

**Indexes:** Expression indexes plus GIN for advanced use cases:

```sql
-- Expression index (same concept as SQLite)
CREATE INDEX IF NOT EXISTS idx_products_price
    ON products(((data->>'price')::float));

-- GIN index (PostgreSQL-exclusive, enables @> containment queries)
CREATE INDEX IF NOT EXISTS idx_products_gin
    ON products USING GIN(data jsonb_path_ops);
```

The GIN index is created automatically when a collection is registered with PostgreSQL. It accelerates `In`, `Contains`, and future containment-based queries without requiring per-field index definitions.

**Aggregation:** Fully delegated to PostgreSQL, which can use parallel query execution for large datasets.

**Best for:** Production deployments needing replication and backups, large datasets (millions of documents), complex analytical queries, applications already running PostgreSQL, multi-service architectures where multiple applications access the same data.

***

## Serialization Strategy

Both backends use JSON encoding via `den` struct tags. Documents are serialized to JSON and stored as JSONB in the respective database engine.

| Aspect | SQLite | PostgreSQL |
|---|---|---|
| **Wire format** | JSON -> JSONB (via `jsonb()`) | JSON -> JSONB (native) |
| **Struct tags** | `json:"field"` for name, `den:"index"` for options | `json:"field"` for name, `den:"index"` for options |
| **Null handling** | `den:"omitempty"` | `den:"omitempty"` |
| **Custom encoders** | `json.Marshaler` interface | `json.Marshaler` interface |
| **Binary data (`[]byte`)** | Base64-encoded in JSON (~33% overhead) | Base64-encoded in JSON (~33% overhead) |
| **Storage format** | JSONB BLOB | JSONB |
| **Decode speed** | Fast (JSONB skip parse) | Fast (JSONB skip parse) |

**Tag convention:** The `json` tag sets the serialized field name (the key in JSONB). The `den` tag carries only Den-specific options — no field name:

```go
type Product struct {
    document.Base
    Name  string  `json:"name"  den:"index"`
    Price float64 `json:"price" den:"index"`
}
```

**Custom type encoding** uses the standard `json.Marshaler`/`json.Unmarshaler` interfaces. For most Go types, no custom encoding is needed.

**Null handling** and **omitempty** behavior is consistent across backends — the `den:"omitempty"` option applies regardless of backend. The `OmitEmpty` setting in `DenSettings()` sets the default for all fields.

**Binary data (`[]byte` fields):** Both backends automatically base64-encode `[]byte` fields as part of JSON serialization, adding ~33% storage overhead. This is transparent to the application. For documents with large binary payloads (e.g. images, files), consider storing the binary data externally and keeping only a reference string in the document.

***

## Layer 3: Query Builder, Aggregation & Relations

### Query Builder

The query builder provides a fluent, type-safe API for constructing queries. It produces an abstract `Query` object that the active backend translates into SQL statements.

#### Public API

```go
// Find multiple documents (chainable QuerySet)
products, err := den.NewQuery[Product](ctx, db,
    where.Field("price").Gte(10.0),
    where.Field("category.name").Eq("Electronics"),
).Sort("price", den.Asc).Limit(20).Skip(10).All()

// Find one document
product, err := den.NewQuery[Product](ctx, db,
    where.Field("name").Eq("Widget"),
).First()

// Find by ID (direct lookup, no QuerySet needed)
product, err := den.FindByID[Product](ctx, db, "01HQ3...")

// Count
count, err := den.NewQuery[Product](ctx, db,
    where.Field("price").Gt(100),
).Count()

// Exists
exists, err := den.NewQuery[Product](ctx, db,
    where.Field("sku").Eq("ABC123"),
).Exists()

// Iterate with range (lazy, streaming)
for doc, err := range den.NewQuery[Product](ctx, db).Iter() {
    if err != nil { return err }
    fmt.Println(doc.Name)
}
```

**Cursor-based pagination:**

For efficient pagination over large result sets, Den supports cursor-based pagination as an alternative to `Skip`. A cursor is the ID of the last document on the previous page:

```go
// First page
page1, err := den.NewQuery[Entry](ctx, db,
    where.Field("read_at").IsNil(),
).Sort("published", den.Desc).Limit(20).All()

// Next page: pass the last document's ID as cursor
lastID := page1[len(page1)-1].ID
page2, err := den.NewQuery[Entry](ctx, db,
    where.Field("read_at").IsNil(),
).Sort("published", den.Desc).After(lastID).Limit(20).All()

// Previous page (backward pagination)
firstID := page2[0].ID
prevPage, err := den.NewQuery[Entry](ctx, db,
    where.Field("read_at").IsNil(),
).Sort("published", den.Desc).Before(firstID).Limit(20).All()
```

`Skip(n)` still works for small offsets but degrades at high page numbers (O(n) skip cost). `After` / `Before` are O(log n) regardless of position because they translate to:

* SQLite: `WHERE (sort_field, id) < (?, ?)` using a row-value comparison
* PostgreSQL: Same as SQLite, optimized by the query planner with index-only scans

#### Where Conditions

```go
package where

// Comparison operators
Field("price").Eq(10)         // field == value
Field("price").Ne(10)         // field != value
Field("price").Gt(10)         // field > value
Field("price").Gte(10)        // field >= value
Field("price").Lt(10)         // field < value
Field("price").Lte(10)        // field <= value

// Null checks
Field("read_at").IsNil()      // field is null / not set
Field("read_at").IsNotNil()   // field is not null / is set

// Set operators
Field("status").In("active", "pending")
Field("status").NotIn("deleted")

// Array operators
Field("tags").Contains("golang")          // array contains value
Field("tags").ContainsAny("golang", "go") // array contains any of these values
Field("tags").ContainsAll("golang", "go") // array contains all of these values

// Logical operators
where.And(
    Field("price").Gt(10),
    Field("price").Lt(100),
)
where.Or(
    Field("status").Eq("active"),
    Field("featured").Eq(true),
)
where.Not(Field("deleted").Eq(true))

// Nested field access (dot notation)
Field("address.city").Eq("Berlin")
Field("tags.0").Eq("featured")         // array index access
```

**Null check implementation per backend:**

* SQLite: `json_extract(data, '$.read_at') IS NULL` / `IS NOT NULL`
* PostgreSQL: `data->>'read_at' IS NULL` / `IS NOT NULL`

**Array Contains implementation per backend:**

* SQLite: `EXISTS (SELECT 1 FROM json_each(json_extract(data, '$.tags')) WHERE value = ?)`
* PostgreSQL: `data->'tags' @> '["golang"]'::jsonb` (leverages GIN index if available)

#### Query Execution Strategy

Query execution is delegated to the active backend. Both backends translate Den queries into SQL and rely on their respective query planners:

1. `FindByID` -> `SELECT data FROM collection WHERE id = ?` — O(1)
2. All other queries -> generated SQL with `json_extract()` (SQLite) or JSONB operators (PostgreSQL), leveraging the SQL query planner to pick the optimal index
3. The SQL query planner handles compound conditions, index selection, and join ordering automatically — no manual optimization needed in Den

PostgreSQL has a slight advantage for complex queries due to its more sophisticated query planner and GIN index support. SQLite compensates with zero-latency in-process execution.

#### Projections

When only a subset of a document's fields is needed, projections reduce overhead. Both backends can use `json_extract()` or JSONB operators to fetch only specific fields directly in SQL, reducing both I/O and decode cost.

```go
// Define a projection struct with only the fields you need
type ProductSummary struct {
    Name  string  `den:"name"`
    Price float64 `den:"price"`
}

// Use Project() to decode into the projection type
err := den.NewQuery[Product](ctx, db,
    where.Field("category.name").Eq("Chocolate"),
).Project(&summaries)
```

For projections that rename or extract nested fields, use `den` struct tags:

```go
type ProductView struct {
    Name         string `den:"name"`
    CategoryName string `den:"from:category.name"` // extract nested field
}

err := den.NewQuery[Product](ctx, db).Project(&views)
```

#### Refresh / Sync

A document that has been loaded can be refreshed from the database to pick up changes made by other goroutines or processes:

```go
product, _ := den.FindByID[Product](db, "01HQ3...")

// ... time passes, another process may have updated the document ...

// Re-read the document from the backend by its ID, replacing all field values
err := den.Refresh(db, product)
```

`Refresh` performs a `FindByID` and overwrites all fields on the existing struct pointer. If the document has been deleted in the meantime, `ErrNotFound` is returned.

### Aggregation

Den provides aggregation methods inspired by Beanie's shorthand aggregation API. These are fluent methods chained onto a query, allowing filtering and aggregation in a single expression.

#### Scalar Aggregations

Scalar aggregations return a single value computed over all matching documents:

```go
// Average price of all chocolate products
avgPrice, err := den.NewQuery[Product](ctx, db,
    where.Field("category.name").Eq("Chocolate"),
).Avg("price")

// Sum over the whole collection
totalRevenue, err := den.NewQuery[Product](ctx, db).Sum("price")

// Min / Max
cheapest, err := den.NewQuery[Product](ctx, db).Min("price")
mostExpensive, err := den.NewQuery[Product](ctx, db).Max("price")

// Count (with or without filter)
activeCount, err := den.NewQuery[Product](ctx, db,
    where.Field("status").Eq("active"),
).Count()

totalCount, err := den.NewQuery[Product](ctx, db).Count()
```

**Implementation:** Scalar aggregations are delegated to the backend's native SQL aggregation functions (`AVG()`, `SUM()`, `MIN()`, `MAX()`, `COUNT()`). They benefit from the same index optimizations as regular queries — if the filter targets an indexed field, only matching documents are scanned.

**Return types:**

* `Avg`, `Sum` -> `float64`
* `Min`, `Max` -> `float64` (for numeric fields) or `string` (for string fields) — determined by the field's actual type
* `Count` -> `int64`

#### GroupBy Aggregation

For grouped aggregations, Den provides a `GroupBy` builder that collects results into a user-defined struct:

```go
type CategoryStats struct {
    Category string  `den:"group_key"`
    AvgPrice float64 `den:"avg:price"`
    Total    float64 `den:"sum:price"`
    Count    int64   `den:"count"`
    MinPrice float64 `den:"min:price"`
    MaxPrice float64 `den:"max:price"`
}

err := den.NewQuery[Product](ctx, db,
    where.Field("status").Eq("active"),
).GroupBy("category.name").Into(&results)
```

The `Into` target struct uses `den` struct tags to declare which aggregation function applies to which field:

| Tag | Meaning |
|---|---|
| `den:"group_key"` | This field receives the group key value |
| `den:"avg:fieldname"` | Average of `fieldname` within the group |
| `den:"sum:fieldname"` | Sum of `fieldname` within the group |
| `den:"min:fieldname"` | Minimum of `fieldname` within the group |
| `den:"max:fieldname"` | Maximum of `fieldname` within the group |
| `den:"count"` | Number of documents in the group |

**Implementation:** GroupBy is delegated to the backend's native SQL `GROUP BY` with aggregate functions. Results are mapped into the target struct via reflection.

#### Aggregation Non-Goals

Den does **not** implement:

* Multi-stage aggregation pipelines (MongoDB's `$group` -> `$project` -> `$sort` chains)
* `$unwind` (array flattening)
* `$lookup` within aggregation (cross-collection joins — use `Link[T]` with `FetchLinks` instead)
* `$facet` (parallel pipeline branches)
* Computed/derived fields within aggregation expressions

These features require an expression evaluator that would be disproportionate to Den's scope as an ODM. Applications needing complex analytical queries should export data to an appropriate tool.

### Relations

Relations are modeled via the generic `Link[T]` type, inspired by Beanie's `Link[Document]`.

#### Link Type

```go
// Link represents a reference to a document in another collection.
// It stores only the ID when serialized, but can hold the full document when fetched.
type Link[T any] struct {
    // ID is the referenced document's identifier.
    // This is what gets persisted to storage.
    ID string

    // Value holds the resolved document. It is nil until the link is fetched
    // (either via FetchLinks option on a query, or via explicit FetchLink call).
    Value  *T

    loaded bool
}

// NewLink creates a Link from an existing document, extracting its ID.
func NewLink[T Document](doc *T) Link[T]

// IsLoaded reports whether the linked document has been fetched.
func (l Link[T]) IsLoaded() bool
```

**Serialization:** When a document containing `Link[T]` fields is written to storage, only the `ID` string is stored in the JSON. The `Value` and `loaded` fields are transient.

#### Supported Relation Patterns

```go
type Door struct {
    doc.Base
    Height int `den:"height"`
    Width  int `den:"width"`
}

type Window struct {
    doc.Base
    X int `den:"x"`
    Y int `den:"y"`
}

type House struct {
    doc.Base
    Name    string           `den:"name"`
    Door    Link[Door]       `den:"door"`       // One-to-one
    Windows []Link[Window]   `den:"windows"`    // One-to-many
}
```

**Supported patterns:**

* `Link[T]` – single reference (one-to-one)
* `[]Link[T]` – list of references (one-to-many)

**Not supported:**

* Many-to-many (can be modeled via an intermediary document)

**Back-references** are supported via `BackLinks`:

```go
// Find all Houses that reference a specific Door
houses, err := den.BackLinks[House](ctx, db, "door", doorID)
```

#### Write Rules

```go
type LinkRule int

const (
    // LinkIgnore performs no cascading — only the root document is written.
    LinkIgnore LinkRule = iota

    // LinkWrite cascades write operations to all linked documents.
    // New linked documents are inserted, existing ones are replaced.
    LinkWrite
)
```

Usage:

```go
house := &House{
    Name: "Lakehouse",
    Door: den.NewLink(&Door{Height: 2, Width: 1}),
    Windows: []Link[Window]{
        den.NewLink(&Window{X: 100, Y: 50}),
    },
}

// Cascade: saves House, Door, and all Windows
err := den.Insert(ctx, db, house, den.WithLinkRule(den.LinkWrite))

// No cascade: saves only the House; linked documents must already exist
err := den.Insert(ctx, db, house, den.WithLinkRule(den.LinkIgnore))
```

#### Fetch Modes

```go
// Eager fetch: resolve all links during the query
houses, err := den.NewQuery[House](ctx, db,
    where.Field("name").Eq("Lakehouse"),
).WithFetchLinks().All()
// houses[0].Door.Value != nil
// houses[0].Windows[0].Value != nil

// Lazy fetch (default): links contain only IDs
houses, err := den.NewQuery[House](ctx, db,
    where.Field("name").Eq("Lakehouse"),
).All()
// houses[0].Door.Value == nil
// houses[0].Door.ID == "door_01HQ3..."

// On-demand: fetch a single link field
err := den.FetchLink(ctx, db, house, "door")

// On-demand: fetch all link fields
err := den.FetchAllLinks(ctx, db, house)
```

**Implementation:** Eager fetch performs additional lookups within the same backend transaction for read consistency. For the SQLite backend, each link resolution is a direct in-process lookup — no network overhead. For the PostgreSQL backend, link fetches are batched into a single query where possible.

#### Delete Rules

```go
const (
    // LinkIgnore keeps linked documents when the parent is deleted.
    LinkIgnore LinkRule = iota

    // LinkDelete cascades deletion to all linked documents.
    LinkDelete
)
```

Usage:

```go
// Delete house and all linked door + windows
err := den.Delete(ctx, db, house, den.WithLinkRule(den.LinkDelete))

// Delete only the house; door and windows remain
err := den.Delete(ctx, db, house, den.WithLinkRule(den.LinkIgnore))
```

#### Nesting Depth Limit

When documents contain links to documents that themselves contain links, eager fetching can cause deep recursion — or infinite loops with circular references. Den limits the maximum nesting depth, defaulting to **3 levels**.

**Global setting per document type:**

```go
func (h House) DenSettings() den.Settings {
    return den.Settings{
        MaxNestingDepth: 2, // fetch at most 2 levels of linked documents
    }
}
```

**Per-field override:**

```go
func (h House) DenSettings() den.Settings {
    return den.Settings{
        MaxNestingDepth: 3,
        NestingDepthPerField: map[string]int{
            "door":    1, // door links are shallow, only 1 level
            "windows": 2, // windows can go 2 levels deep
        },
    }
}
```

**Per-query override:**

```go
houses, err := den.NewQuery[House](ctx, db).
    WithFetchLinks().WithNestingDepth(1).All()
```

**Implementation:** The link resolver maintains a depth counter that is decremented with each recursive fetch. When the counter reaches zero, remaining `Link[T]` fields are left unresolved (ID only, `Value == nil`). This prevents infinite loops and bounds resource consumption.

***

## Layer 4: Lifecycle & Middleware

### Lifecycle Hooks

Documents can implement hook interfaces to run logic before or after database operations. Hooks are called within the same transaction.

```go
// Hook interfaces — implement any combination on your document struct.
type BeforeInsert interface {
    BeforeInsert(ctx context.Context) error
}

type AfterInsert interface {
    AfterInsert(ctx context.Context) error
}

type BeforeUpdate interface {
    BeforeUpdate(ctx context.Context) error
}

type AfterUpdate interface {
    AfterUpdate(ctx context.Context) error
}

type BeforeDelete interface {
    BeforeDelete(ctx context.Context) error
}

type AfterDelete interface {
    AfterDelete(ctx context.Context) error
}

type BeforeSave interface {
    BeforeSave(ctx context.Context) error  // Called by both Insert and Update
}

type AfterSave interface {
    AfterSave(ctx context.Context) error
}

// Validation hook — called before any write if implemented
type ValidateOnSave interface {
    Validate() error
}
```

**Example:**

```go
type Article struct {
    doc.Base
    Title     string    `den:"title"`
    Slug      string    `den:"slug"`
    Body      string    `den:"body"`
    WordCount int       `den:"word_count"`
}

func (a *Article) BeforeSave(ctx context.Context) error {
    a.Slug = slugify(a.Title)
    a.WordCount = len(strings.Fields(a.Body))
    return nil
}

func (a *Article) Validate() error {
    if a.Title == "" {
        return errors.New("title is required")
    }
    return nil
}
```

**Execution order for `den.Insert()`:**

1. `BeforeInsert.BeforeInsert(ctx)` (if implemented — mutating hook)
2. `BeforeSave.BeforeSave(ctx)` (if implemented — mutating hook)
3. Struct tag validation (`go-playground/validator`, if enabled)
4. `Validator.Validate()` (if implemented — runs on final post-hook state)
5. Encode and write document + indexes via the backend
6. `AfterInsert.AfterInsert(ctx)` (if implemented)
7. `AfterSave.AfterSave(ctx)` (if implemented)

Mutating hooks run before validation so they can populate defaults, compute derived fields, and normalize values before constraints are checked. If any hook or validation step returns an error, the operation is aborted.

### Revision Control (Optimistic Concurrency)

Documents can opt into revision tracking for optimistic concurrency control, preventing silent data loss from concurrent modifications.

```go
type Settings struct {
    UseRevision bool
}

type Product struct {
    doc.Base
    Name  string  `den:"name"`
    Price float64 `den:"price"`
}

func (p Product) DenSettings() den.Settings {
    return den.Settings{UseRevision: true}
}
```

**Mechanism:**

* Each document stores a `_rev` field (a random string, regenerated on each write)
* On `Update`, Den checks that the document's `_rev` matches what is stored in the backend
* If it doesn't match (another process updated the doc), `ErrRevisionConflict` is returned
* The check-and-write happens atomically within a single backend transaction

```go
p, _ := den.FindByID[Product](ctx, db, "prod_001")
p.Price = 29.99

// If someone else updated this document since we read it:
err := den.Update(ctx, db, p)
// err == den.ErrRevisionConflict

// To force-write regardless:
err := den.Update(ctx, db, p, den.IgnoreRevision())
```

### Query Cache (Planned)

An optional in-process LRU cache for repeated queries is planned but not yet implemented. The `Settings` struct already includes `UseCache`, `CacheCapacity`, and `CacheExpiration` fields for future use.

### Soft Delete

Instead of permanently removing documents, soft delete marks them with a `DeletedAt` timestamp. Soft-deleted documents are automatically excluded from normal queries but remain in storage.

**Enabling soft delete:**

Documents opt in by embedding `doc.SoftBase` instead of `doc.Base`:

```go
package doc

type SoftBase struct {
    Base
    DeletedAt *time.Time `den:"_deleted_at,omitempty"`
}

func (s SoftBase) IsDeleted() bool {
    return s.DeletedAt != nil
}
```

```go
type Product struct {
    doc.SoftBase  // instead of doc.Base
    Name  string  `den:"name"`
    Price float64 `den:"price"`
}
```

**Behavior:**

```go
// Soft delete: sets DeletedAt, document remains in storage
err := den.Delete(ctx, db, product)
product.IsDeleted() // true

// Standard queries automatically exclude soft-deleted documents
products, _ := den.NewQuery[Product](ctx, db).All() // only non-deleted

// Explicitly include soft-deleted documents
all, _ := den.NewQuery[Product](ctx, db).IncludeDeleted().All()

// Permanently remove from storage
err := den.HardDelete(ctx, db, product)
```

**Implementation:** When Den detects that a document type embeds `SoftBase` (via reflection at registration time), it:

1. Rewrites `Delete()` to set `DeletedAt = time.Now()` + `Replace()` instead of removing the row
2. Injects an automatic `where.Field("_deleted_at").IsNil()` condition into all `Find` queries for that collection
3. Provides `HardDelete()` that performs an actual permanent deletion from the backend
4. Provides `IncludeDeleted(true)` query option to bypass the automatic filter

### Change Tracking

Den supports per-document change tracking via `document.TrackedBase`. Documents that embed `TrackedBase` (instead of `Base`) automatically capture a snapshot of their serialized state after every load or write. This enables `IsChanged`, `GetChanges`, and `Rollback`.

**Enabling change tracking:**

```go
type Product struct {
    document.TrackedBase  // instead of document.Base
    Name  string  `json:"name"  den:"index"`
    Price float64 `json:"price" den:"index"`
}

// For documents that need both tracking and soft-delete:
type AuditLog struct {
    document.TrackedSoftBase
    Action string `json:"action"`
}
```

**Change tracking API:**

```go
p, _ := den.NewQuery[Product](ctx, db, where.Field("name").Eq("Widget")).First()

den.IsChanged(db, p)    // false
den.GetChanges(db, p)   // map[string]FieldChange{} (empty)

p.Price = 29.99

den.IsChanged(db, p)    // true
den.GetChanges(db, p)   // map[string]FieldChange{"price": {Before: 19.99, After: 29.99}}

// Undo local changes, restore to last-saved state
den.Rollback(db, p)

den.IsChanged(db, p)    // false
p.Price                  // back to original value
```

**Implementation:** When a document implements the `Trackable` interface (which `TrackedBase` and `TrackedSoftBase` satisfy), Den stores a byte-level snapshot of the serialized JSON after every `Insert`, `Update`, `FindByID`, or query iteration. `IsChanged` re-encodes the current state and compares bytes. `GetChanges` diffs the two JSON representations. `Rollback` deserializes the stored snapshot back into the struct.

***

## Migrations

Den provides a migration framework for evolving document schemas over time. While documents are schema-flexible by nature (new fields with zero values are silently accepted), explicit migrations are needed for field renames, type changes, and data transformations.

### Migration Definition

Migrations are Go functions registered with a version identifier:

```go
package main

import "your/module/den/migrate"

func setupMigrations() *migrate.Registry {
    r := migrate.NewRegistry()

    r.Register("20250402_001_rename_name_to_title", migrate.Migration{
        Forward: func(ctx context.Context, tx *den.Tx) error {
            // Iterative migration using QuerySet.Iter()
            for note, err := range den.NewQuery[OldNote](ctx, db).Iter() {
                if err != nil { return err }
                note.Title = note.Name
                if err := den.TxUpdate(tx, note); err != nil {
                    return err
                }
            }
            return nil
        },
        Backward: func(ctx context.Context, tx *den.Tx) error {
            // reverse migration
            return nil
        },
    })

    return r
}
```

### Migration Runner

```go
r := setupMigrations()

// Run all pending forward migrations
err := r.Up(ctx, db)

// Run one forward migration
err := r.UpOne(ctx, db)

// Roll back one migration
err := r.DownOne(ctx, db)

// Roll back all migrations
err := r.Down(ctx, db)
```

**Tracking:** Den stores migration state in a `_den_migrations` table. Each applied migration is recorded with its version string and timestamp. `migrate.Up()` compares registered migrations against the log to determine which are pending.

**Transaction safety:** Each migration runs within a Den transaction. If the migration function returns an error, the transaction is rolled back and the migration is marked as failed. Subsequent migrations are not executed.

### Iterating Documents in Migrations

For iterative migrations, use `QuerySet.Iter()` which returns a Go range-compatible iterator that streams documents without loading them all into memory:

```go
for doc, err := range den.NewQuery[Product](ctx, db).Iter() {
    if err != nil { return err }
    // modify and update within the transaction
}
```

***

## Layer 5: Registration & Initialization

### Opening a Database

```go
// Open a database backed by an on-disk SQLite file
db, err := den.Open(sqlite.Open("./data/myapp.db"))
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### Registering Document Types

All document types must be registered before use. Registration performs:

1. Reflection-based analysis of the struct (field names, types, tags)
2. Collection metadata creation or verification
3. Index creation or verification (additive — new indexes are created, existing ones are kept)

```go
err := den.Register(ctx, db, 
    &Product{},
    &Category{},
    &House{},
    &Door{},
    &Window{},
)
```

### Document Base Type

All documents embed `doc.Base`, which provides the standard fields:

```go
package document

type Base struct {
    ID        string    `json:"_id"`
    CreatedAt time.Time `json:"_created_at"`
    UpdatedAt time.Time `json:"_updated_at"`
    Rev       string    `json:"_rev,omitempty"` // populated when UseRevision is enabled
}

// TrackedBase adds change tracking (snapshot-based).
// Embed this instead of Base to enable IsChanged/GetChanges/Rollback.
type TrackedBase struct {
    Base
    snapshot []byte // not serialized, set automatically after load/write
}
```

**ID generation:**

* Default: [ULID](https://github.com/oklog/ulid) — lexicographically sortable, timestamp-ordered, 26 characters
* ULIDs are preferred over UUIDs because they sort chronologically, which benefits sorted storage engines (SQLite's B-tree, PostgreSQL's indexes) and improves write and scan performance across both backends
* Users can set `ID` manually before insert; if empty, Den generates a ULID

### Settings via Interface

Document-level settings are configured by implementing the `DenSettable` interface:

```go
type DenSettable interface {
    DenSettings() den.Settings
}

type Settings struct {
    // CollectionName overrides the auto-derived collection name.
    CollectionName string

    // OmitEmpty controls default null handling. When true, zero-value fields
    // are omitted from storage unless the field has an explicit den tag
    // without omitempty. Default: false (all fields stored).
    OmitEmpty bool

    // UseRevision enables optimistic concurrency control.
    UseRevision bool

    // UseCache enables LRU query caching (planned, not yet implemented).
    UseCache        bool
    CacheCapacity   int
    CacheExpiration time.Duration

    // NestingDepthPerField overrides the default nesting depth for specific Link fields.
    NestingDepthPerField map[string]int

    // Indexes defines secondary indexes for this collection.
    // Indexes can also be defined via struct tags.
    Indexes []IndexDefinition
}
```

### Index Definition

Indexes can be defined in two ways:

**Via struct tags:**

```go
type Product struct {
    doc.Base
    Name  string  `json:"name"  den:"index"`
    SKU   string  `json:"sku"   den:"unique"`
    Price float64 `json:"price" den:"index"`
}
```

**Via Settings (for compound indexes):**

```go
func (p Product) DenSettings() den.Settings {
    return den.Settings{
        Indexes: []den.IndexDefinition{
            {Name: "idx_category_price", Fields: []string{"category", "price"}},
        },
    }
}
```

### Nullable Unique Constraints

When a pointer field (e.g. `*string`) is tagged with `den:"unique"`, Den only enforces uniqueness for non-nil values. Multiple documents may have `nil` for the same unique field without conflict.

```go
type User struct {
    doc.Base
    Username string  `json:"username" den:"unique"`          // always required, always unique
    Email    *string `json:"email,omitempty" den:"unique"`   // optional, unique when set
}

// These two users can coexist — both have nil Email:
den.Insert(db, &User{Username: "alice"})       // Email: nil ✓
den.Insert(db, &User{Username: "bob"})         // Email: nil ✓

// But two users with the same Email value cannot:
den.Insert(db, &User{Username: "carol", Email: ptr("carol@example.com")}) // ✓
den.Insert(db, &User{Username: "dave",  Email: ptr("carol@example.com")}) // ErrDuplicate
```

**Implementation per backend:**

* **SQLite**: `CREATE UNIQUE INDEX ... ON table(json_extract(data, '$.email')) WHERE json_extract(data, '$.email') IS NOT NULL` — a partial index.
* **PostgreSQL**: Same as SQLite — partial unique index with `WHERE ... IS NOT NULL`.

### Collection Metadata API

Den's collection registry stores structural metadata about every registered document type (field names, types, indexes, relations). This metadata is exposed via a public API for use by admin panels, introspection tools, and code generators.

```go
// Get metadata for a registered collection
meta := den.Meta[Note](db)

meta.Name        // "notes"
meta.Fields      // []FieldMeta{{Name: "title", Type: "string", Indexed: true}, ...}
meta.Indexes     // []IndexDefinition{{Name: "idx_title", Fields: ["title"], Unique: false}}
meta.Links       // []LinkMeta{{Field: "author", Target: "authors", IsSlice: false}}
meta.HasSoftBase // true if doc.SoftBase is embedded
meta.Settings    // the resolved DenSettings for this collection
```

```go
// List all registered collection names
names := den.Collections(db) // []string{"notes", "users", "jobs", ...}
```

This enables Burrow's admin contrib app to auto-generate list and detail views by iterating over fields and their types — the same approach the current `ModelAdmin` takes with Bun's reflection.

### Healthcheck

Den exposes a liveness check on the database instance:

```go
err := db.Ping(ctx)
```

`Ping` delegates to the backend's `Ping` method:

* **SQLite**: Executes `SELECT 1`.
* **PostgreSQL**: Calls the pgx connection pool's `Ping`.

This integrates directly with Burrow's healthcheck contrib app:

```go
// In healthcheck handler:
status := "ok"
if err := db.Ping(r.Context()); err != nil {
    status = "error"
}
```

***

## Transactions

Den exposes explicit transactions for operations that span multiple documents or collections.

```go
err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    // All operations in this closure use the same backend transaction.
    // Reads see a consistent snapshot.
    
    sender, err := den.TxFindByID[Account](tx, senderID)
    if err != nil {
        return err
    }
    
    receiver, err := den.TxFindByID[Account](tx, receiverID)
    if err != nil {
        return err
    }
    
    sender.Balance -= amount
    receiver.Balance += amount
    
    if err := den.TxUpdate(tx, sender); err != nil {
        return err
    }
    if err := den.TxUpdate(tx, receiver); err != nil {
        return err
    }
    
    // Returning nil commits the transaction.
    // Returning an error rolls it back.
    return nil
})
```

**Implementation:**

* `den.Tx` wraps the active backend's `Transaction` (SQLite: `BEGIN`/`COMMIT`, PostgreSQL: `BEGIN`/`COMMIT`)
* The transaction provides consistent reads (snapshot isolation) and atomic writes
* On success, the transaction is committed; on error, it is rolled back
* Concurrent transaction behavior depends on the backend: SQLite serializes writers, PostgreSQL uses MVCC with configurable isolation levels
* For application-level conflict detection regardless of backend, use revision control

***

## CRUD Operations Reference

### Insert

```go
// Insert a single document. ID is auto-generated if empty.
err := den.Insert(db, &product)

// Insert multiple documents in a single batch.
err := den.InsertMany(db, []*Product{&p1, &p2, &p3})
```

### Query

```go
// Find multiple documents matching conditions (chainable QuerySet).
products, err := den.NewQuery[Product](ctx, db, ...conditions).All()

// Find the first matching document.
product, err := den.NewQuery[Product](ctx, db, ...conditions).First()

// Find by ID (direct key lookup, fastest path).
product, err := den.FindByID[Product](ctx, db, "01HQ3...")

// Find by multiple IDs.
products, err := den.FindByIDs[Product](ctx, db, ids)

// Find with total count (for offset-based pagination).
notes, total, err := den.NewQuery[Note](ctx, db,
    where.Field("user").Eq(userID),
).Sort("_created_at", den.Desc).Limit(20).Skip(40).AllWithCount()
// total = 347 -> use to compute TotalPages, HasMore

// Count and Exists
count, err := den.NewQuery[Product](ctx, db, ...conditions).Count()
exists, err := den.NewQuery[Product](ctx, db, ...conditions).Exists()

// Lazy iteration (streaming, memory-efficient)
for doc, err := range den.NewQuery[Product](ctx, db).Iter() {
    if err != nil { return err }
    // process doc
}
```

### Update

```go
// Update a document (must have an ID, full document write).
err := den.Update(ctx, db, &product)

// Bulk update: update fields on all matching documents.
count, err := den.NewQuery[Product](ctx, db,
    where.Field("category").Eq("old"),
).Update(den.SetFields{"category": "new"})
```

### Find and Modify

Atomic find-and-update in a single operation. Finds the first document matching the query, applies the field updates, and returns the modified document. The entire operation runs in a transaction.

```go
// Atomically claim the next pending job (job queue pattern)
job, err := den.FindOneAndUpdate[Job](ctx, db,
    den.SetFields{
        "status":     "running",
        "started_at": time.Now(),
    },
    where.Field("status").Eq("pending"),
    where.Field("scheduled_at").Lte(time.Now()),
)
// job is the updated document, or ErrNotFound if no match
```

### Delete

```go
// Delete a specific document.
err := den.Delete(ctx, db, &product)

// Delete all matching a query.
count, err := den.DeleteMany[Product](ctx, db,
    []where.Condition{where.Field("status").Eq("archived")},
)
```

***

## Error Handling

Den uses typed sentinel errors for predictable error handling:

```go
package den

var (
    // ErrNotFound is returned when a document lookup yields no result.
    ErrNotFound = errors.New("den: document not found")

    // ErrDuplicate is returned when a unique index constraint is violated.
    ErrDuplicate = errors.New("den: duplicate key")

    // ErrRevisionConflict is returned when optimistic concurrency check fails.
    ErrRevisionConflict = errors.New("den: revision conflict")

    // ErrNotRegistered is returned when operating on an unregistered document type.
    ErrNotRegistered = errors.New("den: document type not registered")

    // ErrValidation is returned when a ValidateOnSave hook fails.
    ErrValidation = errors.New("den: validation failed")

    // ErrTransactionFailed is returned when a transaction could not be committed.
    ErrTransactionFailed = errors.New("den: transaction failed")

    // ErrNoSnapshot is returned by Rollback when the document has no stored snapshot
    // (i.e. it was never loaded from the database or doesn't embed TrackedBase).
    ErrNoSnapshot = errors.New("den: no snapshot")

    // ErrMigrationFailed is returned when a migration function returns an error.
    // The wrapped error contains the migration version and the original error.
    ErrMigrationFailed = errors.New("den: migration failed")
)
```

All errors wrap the sentinel with additional context via `fmt.Errorf("...: %w", err)` so that `errors.Is()` works.

***

## Testing Support

Den provides a `dentest` package for easy test setup:

```go
package myapp_test

import (
    "testing"
    "your/module/den"
    "your/module/den/dentest"
)

func TestProductInsert(t *testing.T) {
    // Creates a file-backed SQLite database in a temp dir, pre-registers the given types
    db := dentest.MustOpen(t, &Product{}, &Category{})
    // db is automatically closed when the test ends (via t.Cleanup)

    ctx := context.Background()
    p := &Product{Name: "Test", Price: 9.99}
    if err := den.Insert(ctx, db, p); err != nil {
        t.Fatal(err)
    }

    found, err := den.FindByID[Product](ctx, db, p.ID)
    if err != nil {
        t.Fatal(err)
    }
    if found.Name != "Test" {
        t.Errorf("got %q, want %q", found.Name, "Test")
    }
}
```

***

## Burrow Integration

In the context of Burrow's pluggable app architecture, Den serves as the default data layer. Each Burrow app registers its document types during app initialization:

```go
// In a Burrow app's setup
type BlogApp struct{}

func (a *BlogApp) Register(b *burrow.Burrow) {
    // Register document types with Den
    den.Register(b.DB(),
        &Article{},
        &Comment{},
        &Author{},
    )

    // Register routes, etc.
    b.GET("/articles", a.ListArticles)
}
```

This mirrors Django's pattern where each app brings its own models, but without the schema migration overhead — documents are schema-flexible by nature.

***

## Dependencies

### Core (always required)

| Dependency | Purpose | License |
|---|---|---|
| `github.com/oklog/ulid/v2` | ID generation | Apache-2.0 |

### Per Backend

| Dependency | Backend | License |
|---|---|---|
| `modernc.org/sqlite` | SQLite (pure Go, no CGO) | BSD-3-Clause |
| `github.com/jackc/pgx/v5` | PostgreSQL | MIT |

***

## Implementation Status

All core features are implemented:

* \[x] Backend interface (ReadWriter, Backend, Transaction, Iterator)
* \[x] SQLite and PostgreSQL backends with prepared statement / SQL string caching
* \[x] Document base types (Base, TrackedBase, SoftBase, TrackedSoftBase)
* \[x] Full CRUD: Insert, Update, Delete, FindByID, FindByIDs, InsertMany, DeleteMany
* \[x] Chainable QuerySet with All, First, Count, Exists, AllWithCount, Iter
* \[x] Where conditions (Eq, Ne, Gt, Gte, Lt, Lte, In, NotIn, IsNil, IsNotNil, Contains, ContainsAny, ContainsAll, HasKey, RegExp)
* \[x] Sort, Limit, Skip, cursor-based pagination (After/Before)
* \[x] Native aggregation (Avg, Sum, Min, Max, Count via SQL pushdown)
* \[x] GroupBy with accumulator structs, Project with projection structs
* \[x] Link\[T] relations with cascade write/delete, eager/lazy fetch, BackLinks
* \[x] Lifecycle hooks (BeforeInsert, AfterInsert, BeforeUpdate, AfterUpdate, BeforeDelete, AfterDelete, BeforeSave, AfterSave, Validate)
* \[x] Revision control (optimistic concurrency)
* \[x] Change tracking (TrackedBase: IsChanged, GetChanges, Rollback)
* \[x] Soft delete (SoftBase, HardDelete, IncludeDeleted)
* \[x] Transactions (RunInTransaction, Tx\* functions)
* \[x] FindOneAndUpdate (atomic find + modify)
* \[x] Migration framework (Registry, Up, Down, UpOne, DownOne)
* \[x] Full-text search (FTS5 for SQLite, tsvector for PostgreSQL)
* \[x] Expression indexes, unique indexes, compound indexes, nullable unique
* \[x] dentest package for testing

### Planned

* \[ ] LRU query cache with TTL (Settings fields exist, implementation pending)
* \[ ] Burrow app integration helpers
* \[ ] Benchmarks: SQLite vs. PostgreSQL across workload profiles

***

## Full-Text Search

Den provides full-text search as an opt-in feature. The FTS implementation uses each backend's native capabilities, but the user-facing API is identical regardless of backend.

### User-Facing API

Fields are marked for full-text indexing via `den:"fts"` struct tags:

```go
type Article struct {
    document.Base
    Title string `json:"title" den:"fts"`
    Body  string `json:"body"  den:"fts"`
    Tags  string `json:"tags"`
}
```

Searching uses a unified API:

```go
// Full-text search
articles, err := den.Search[Article](db, "golang embedded database",
    query.Limit(20),
)

// Combined: FTS + regular query conditions
articles, err := den.Search[Article](db, "golang",
    where.Field("tags").Eq("tutorial"),
    query.Sort("_created_at", query.Desc),
)
```

### Backend-Native FTS Implementations

Each backend uses its native FTS engine, requiring no external dependencies:

**SQLite -> FTS5:**

Den automatically creates and maintains FTS5 virtual tables for collections with `den:"fts"` fields:

```sql
-- Auto-generated by Den's SQLite backend during Register()
CREATE VIRTUAL TABLE IF NOT EXISTS articles_fts USING fts5(
    title, body, content=articles, content_rowid=rowid
);

-- Search query generated by den.Search()
SELECT a.data FROM articles a
JOIN articles_fts f ON a.rowid = f.rowid
WHERE articles_fts MATCH ?
ORDER BY rank
LIMIT 20;
```

FTS5 triggers are created to keep the index in sync on insert/update/delete. This is fully atomic with the document write.

**PostgreSQL -> tsvector/tsquery:**

Den uses PostgreSQL's native full-text search with `tsvector` generated columns and GIN indexes:

```sql
-- Auto-generated during Register()
ALTER TABLE articles ADD COLUMN _fts_vector tsvector
    GENERATED ALWAYS AS (to_tsvector('english', coalesce(data->>'title','') || ' ' || coalesce(data->>'body',''))) STORED;
CREATE INDEX idx_articles_fts ON articles USING GIN(_fts_vector);

-- Search query generated by den.Search()
SELECT data FROM articles
WHERE _fts_vector @@ plainto_tsquery('english', $1)
ORDER BY ts_rank(_fts_vector, plainto_tsquery('english', $1)) DESC
LIMIT 20;
```

PostgreSQL FTS supports language-aware stemming, ranking, and phrase search natively.

**Trade-offs:**

* Both implementations are zero-dependency, atomic, and well-integrated with their query planners
* SQLite FTS5 does not support language-aware stemming out of the box (requires ICU extension). PostgreSQL's tsvector has full stemming support.
* For applications that don't use `den:"fts"`, no FTS infrastructure is created

***

## Design Decisions

The following architectural decisions have been made and are final:

**1. Collection naming: Lowercase struct name, no pluralization.**
Auto-derived from the struct name in lowercase (`Note` -> `note`, `Entry` -> `entry`). No automatic pluralization — Go pluralization is unpredictable (`Entry` -> `entrys`? `entries`?). Override via `CollectionName` in `DenSettings()` when needed. Simple, predictable, no surprises.

**2. Embed vs. Link: Guidance in documentation, no enforcement.**
Den does not enforce when to embed vs. link. The documentation provides a clear rule of thumb:

* **Embed** when the data has no independent identity, is always read together with the parent, and is not referenced by other documents (e.g. `Address` inside `User`, `Category` inside `Product`).
* **Link** when the data has its own identity, may be referenced by multiple documents, or is updated independently (e.g. `Author` of a `Post`, `Feed` of an `Entry`).

**3. License: MIT.**
Consistent with Burrow. Framework modifications must be contributed back; applications built on top are unrestricted.

**4. Struct tags: Unified `den` tag for JSON field naming.**
A single `den` tag controls the serialized JSON field name across both backends:

```go
type Product struct {
    document.Base
    Name   string   `json:"name"           den:"index"`
    Price  float64  `json:"price"          den:"index"`
    SKU    string   `json:"sku"            den:"unique"`
    Body   string   `json:"body"           den:"fts"`
    Tags   []string `json:"tags,omitempty" den:"index"`
}
```

The `json` tag sets the serialized field name. The `den` tag carries only options: `index`, `unique`, `fts`, `omitempty`.

**5. PostgreSQL-specific features: Uniform API, opt-in extensions.**
The `Backend` interface and all public Den APIs are strictly uniform across backends. Backend-specific capabilities (GIN containment queries, PostgreSQL FTS via `tsvector`) are used *internally* by Den when the standard API maps to them — e.g. `where.Field("tags").Contains("golang")` automatically uses GIN `@>` on PostgreSQL. Direct access to backend-specific features is available via type assertion on `db.Backend()` for advanced use cases, but is not part of the public API contract.

**6. Default backend: SQLite.**
SQLite is the embedded default — ideal for single-binary deployments and development. PostgreSQL is available for production deployments that need replication and scaling:

```go
db, err := den.Open(sqlite.Open("./data/app.db"))    // embedded default ✓
db, err := den.Open(postgres.Open("postgres://..."))  // production scale ✓
```
