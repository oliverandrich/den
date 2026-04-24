# Queries

## QuerySet

`den.NewQuery[T]` returns a chainable, lazy query builder. No database call is made until you invoke an execution method (`All`, `First`, `Count`, etc.):

```go
products, err := den.NewQuery[Product](db,
    where.Field("price").Gte(10.0),
    where.Field("category.name").Eq("Electronics"),
).Sort("price", den.Asc).Limit(20).Skip(10).All(ctx)
```

## Execution Methods

| Method | Return Type | Description |
|---|---|---|
| `All()` | `[]*T, error` | All matching documents |
| `First()` | `*T, error` | First matching document (`ErrNotFound` if none) |
| `Count()` | `int64, error` | Number of matching documents |
| `Exists()` | `bool, error` | Whether any document matches |
| `AllWithCount()` | `[]*T, int64, error` | Documents plus total count (for offset pagination) |

```go
// First matching document
product, err := den.NewQuery[Product](db,
    where.Field("name").Eq("Widget"),
).First(ctx)

// Count
count, err := den.NewQuery[Product](db,
    where.Field("price").Gt(100),
).Count(ctx)

// Exists
exists, err := den.NewQuery[Product](db,
    where.Field("sku").Eq("ABC123"),
).Exists(ctx)

// All with total count (for pagination UI)
notes, total, err := den.NewQuery[Note](db,
    where.Field("user").Eq(userID),
).Sort("_created_at", den.Desc).Limit(20).Skip(40).AllWithCount(ctx)
// total = 347 -> compute TotalPages, HasMore
```

## Iter -- Streaming Results

`Iter()` returns a Go range-compatible iterator that streams documents without loading them all into memory:

```go
for doc, err := range den.NewQuery[Product](db).Iter(ctx) {
    if err != nil {
        return err
    }
    fmt.Println(doc.Name)
}
```

!!! tip
    Use `Iter()` for large result sets or migrations where loading all documents into memory at once would be impractical.

## Where Conditions

Import the `where` package and build conditions with `Field()`:

```go
import "github.com/oliverandrich/den/where"
```

### Comparison Operators

```go
where.Field("price").Eq(10)    // field == value
where.Field("price").Ne(10)    // field != value
where.Field("price").Gt(10)    // field > value
where.Field("price").Gte(10)   // field >= value
where.Field("price").Lt(10)    // field < value
where.Field("price").Lte(10)   // field <= value
```

### Null Checks

```go
where.Field("read_at").IsNil()      // field is null / not set
where.Field("read_at").IsNotNil()   // field is not null / is set
```

### Set Operators

```go
where.Field("status").In("active", "pending")   // value in set
where.Field("status").NotIn("deleted")           // value not in set
```

### Array Operators

```go
where.Field("tags").Contains("golang")              // array contains value
where.Field("tags").ContainsAny("golang", "go")     // array contains any of these
where.Field("tags").ContainsAll("golang", "go")      // array contains all of these
```

### Map / Object Operators

```go
where.Field("metadata").HasKey("version")   // object has key
```

### Pattern Matching

```go
where.Field("name").RegExp("^Wid.*")   // regex match
```

### String Operators

```go
where.Field("name").StringContains("get")    // field contains substring
where.Field("name").StartsWith("Wid")        // field starts with prefix
where.Field("name").EndsWith("get")          // field ends with suffix
```

### Logical Combinators

Combine conditions with `And`, `Or`, and `Not`:

```go
// AND -- all conditions must match
where.And(
    where.Field("price").Gt(10),
    where.Field("price").Lt(100),
)

// OR -- at least one condition must match
where.Or(
    where.Field("status").Eq("active"),
    where.Field("featured").Eq(true),
)

// NOT -- negate a condition
where.Not(where.Field("deleted").Eq(true))
```

!!! note
    Multiple conditions passed to `NewQuery` are implicitly combined with `AND`. Use `where.Or()` explicitly when you need disjunction.

### Nested Field Access

Use dot notation to query fields in embedded objects:

```go
where.Field("address.city").Eq("Berlin")
where.Field("category.name").Eq("Electronics")
where.Field("tags.0").Eq("featured")   // array index access
```

## Sort, Limit, Skip

```go
den.NewQuery[Product](db).
    Sort("price", den.Asc).    // ascending by price
    Sort("name", den.Desc).    // then descending by name
    Limit(20).                 // at most 20 results
    Skip(40).                  // skip the first 40
    All(ctx)
```

## Cursor-Based Pagination

For large result sets, cursor-based pagination with `After` / `Before` is more efficient than `Skip`:

```go
// First page
page1, err := den.NewQuery[Entry](db,
    where.Field("read_at").IsNil(),
).Sort("published", den.Desc).Limit(20).All(ctx)

// Next page: pass the last document's ID as cursor
lastID := page1[len(page1)-1].ID
page2, err := den.NewQuery[Entry](db,
    where.Field("read_at").IsNil(),
).Sort("published", den.Desc).After(lastID).Limit(20).All(ctx)

// Previous page (backward pagination)
firstID := page2[0].ID
prevPage, err := den.NewQuery[Entry](db,
    where.Field("read_at").IsNil(),
).Sort("published", den.Desc).Before(firstID).Limit(20).All(ctx)
```

!!! tip
    `Skip(n)` works for small offsets but degrades at high page numbers (O(n) skip cost). `After` / `Before` use row-value comparisons like `WHERE (sort_field, id) < (?, ?)`, giving O(log n) performance regardless of position. Always prefer cursor-based pagination for user-facing paginated lists.

## Projections

When you only need a subset of fields, projections reduce I/O and decode cost. Define a projection struct with `den` tags:

```go
type ProductSummary struct {
    Name  string  `json:"name"`
    Price float64 `json:"price"`
}

var summaries []ProductSummary
err := den.NewQuery[Product](db,
    where.Field("category.name").Eq("Chocolate"),
).Project(ctx, &summaries)
```

For projections that extract nested fields, use `den:"from:..."`:

```go
type ProductView struct {
    Name         string `json:"name"`
    CategoryName string `den:"from:category.name"` // extract nested field
}

var views []ProductView
err := den.NewQuery[Product](db).Project(ctx, &views)
```

## Query Options

### WithFetchLinks

Eagerly resolve all `Link[T]` fields during the query:

```go
houses, err := den.NewQuery[House](db).WithFetchLinks().All(ctx)
// houses[0].Door.Value != nil
// houses[0].Windows[0].Value != nil
```

`.All(ctx)` runs one batched `WHERE _id IN (…)` per target type per nesting level and deduplicates ids across parents — shared targets are fetched once and the pointer is shared. `.Iter(ctx)` keeps its per-row resolver to preserve streaming. See the [Relations guide](relations.md#eager-withfetchlinks) for the full behavior.

### WithNestingDepth

Control how deep link resolution recurses (default: 3):

```go
houses, err := den.NewQuery[House](db).
    WithFetchLinks().WithNestingDepth(2).All(ctx)
```

Recursion runs on `.All` / `.AllWithCount` / `.Search`; streaming `.Iter` only resolves the direct level.

### IncludeDeleted

Include soft-deleted documents in results (only relevant for types embedding `document.SoftDelete`):

```go
all, err := den.NewQuery[Product](db).IncludeDeleted().All(ctx)
```

## Full-Text Search

For fields tagged with `den:"fts"`, use the `Search` method:

```go
results, err := den.NewQuery[Article](db).Search(ctx, "golang concurrency")
```

=== "SQLite"

    Translates to FTS5 `MATCH` queries against a virtual table.

=== "PostgreSQL"

    Translates to `tsvector` / `tsquery` operations with GIN index acceleration.

## Aggregations

### Scalar Aggregations

```go
avgPrice, err := den.NewQuery[Product](db,
    where.Field("category.name").Eq("Chocolate"),
).Avg(ctx, "price")

totalRevenue, err := den.NewQuery[Product](db).Sum(ctx, "price")
cheapest, err := den.NewQuery[Product](db).Min(ctx, "price")
mostExpensive, err := den.NewQuery[Product](db).Max(ctx, "price")
```

### GroupBy

Collect grouped aggregations into a user-defined struct:

```go
type CategoryStats struct {
    Category string  `den:"group_key"`
    AvgPrice float64 `den:"avg:price"`
    Total    float64 `den:"sum:price"`
    Count    int64   `den:"count"`
    MinPrice float64 `den:"min:price"`
    MaxPrice float64 `den:"max:price"`
}

var results []CategoryStats
err := den.NewQuery[Product](db,
    where.Field("status").Eq("active"),
).GroupBy("category.name").Into(ctx, &results)
```

The `den` tag on the stats struct declares the aggregation function:

| Tag | Meaning |
|---|---|
| `den:"group_key"` | Receives the group key value |
| `den:"avg:field"` | Average of `field` within the group |
| `den:"sum:field"` | Sum of `field` within the group |
| `den:"min:field"` | Minimum of `field` within the group |
| `den:"max:field"` | Maximum of `field` within the group |
| `den:"count"` | Number of documents in the group |
