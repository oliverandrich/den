# Aggregations

Den supports scalar aggregations, grouped aggregations, and projections. Scalar aggregations (Avg, Sum, Min, Max, Count) are pushed down to the database engine via SQL. GroupBy and Project operate in Go memory -- they query matching documents and accumulate or map results in application code.

## Scalar Aggregations

Scalar aggregations return a single value computed over matching documents.

```go
// Average price of chocolate products
avgPrice, err := den.NewQuery[Product](db,
    where.Field("category.name").Eq("Chocolate"),
).Avg(ctx, "price")

// Sum over the entire collection
totalRevenue, err := den.NewQuery[Product](db).Sum(ctx, "price")

// Min / Max
cheapest, err := den.NewQuery[Product](db).Min(ctx, "price")
mostExpensive, err := den.NewQuery[Product](db).Max(ctx, "price")
```

All scalar aggregations accept the same chainable filters as regular queries:

```go
// Average price of active products above $10
avg, err := den.NewQuery[Product](db,
    where.Field("status").Eq("active"),
    where.Field("price").Gt(10),
).Avg(ctx, "price")
```

**Return types:**

| Method | Return type |
|--------|-------------|
| `Avg`  | `float64`   |
| `Sum`  | `float64`   |
| `Min`  | `float64`   |
| `Max`  | `float64`   |
| `Count`| `int64`     |

## Count

Count works with or without filter conditions.

```go
// Total documents in the collection
total, err := den.NewQuery[Product](db).Count(ctx)

// Filtered count
activeCount, err := den.NewQuery[Product](db,
    where.Field("status").Eq("active"),
).Count(ctx)
```

!!! tip
    When you need both results and a total count (e.g. for pagination), use `AllWithCount()` instead of issuing separate `All()` and `Count()` calls.

## GroupBy

`GroupBy` computes aggregations per group and maps results into a user-defined struct. The struct uses `den` tags to declare which aggregation function applies to which document field.

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

### GroupBy Struct Tag Reference

| Tag                  | Meaning                                                   |
|----------------------|-----------------------------------------------------------|
| `den:"group_key"`    | Receives the group key value (shortcut for `group_key:0`) |
| `den:"group_key:N"`  | Receives the N-th group key value (positional)            |
| `den:"avg:field"`    | Average of `field` within the group                       |
| `den:"sum:field"`    | Sum of `field` within the group                           |
| `den:"min:field"`    | Minimum of `field` within the group                       |
| `den:"max:field"`    | Maximum of `field` within the group                       |
| `den:"count"`        | Number of documents in the group                          |

The `field` in each tag refers to the JSON field name on the source document (e.g. `price`, `quantity`). Dot notation for nested fields is supported (e.g. `sum:details.amount`).

### Grouping by Multiple Fields

`GroupBy` accepts a variadic list of fields. Tag each key-carrying struct field with its positional slot `group_key:N`:

```go
type Stats struct {
    Category string  `den:"group_key:0"`
    Region   string  `den:"group_key:1"`
    Count    int64   `den:"count"`
    Total    float64 `den:"sum:price"`
}

var stats []Stats
err := den.NewQuery[Product](db).
    GroupBy("category", "region").
    Into(ctx, &stats)
```

Rules:

- Every slot `0..len(fields)-1` must be claimed by exactly one struct field.
- The unindexed `den:"group_key"` is only accepted when exactly one field is requested; mixing it with `group_key:N` returns an error.
- Two struct fields claiming the same slot, or a slot outside the requested range, also return an error before any SQL runs.

!!! note
    GroupBy queries all matching documents and accumulates results in memory. For large result sets, consider applying filters to reduce the number of documents processed. Scalar aggregations (Avg, Sum, Min, Max, Count) are executed as SQL queries in the database engine without loading documents into memory.

## Projections

When you need a subset of fields rather than full documents, use `Project()` to reduce I/O and decode overhead.

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

For nested fields or field renaming, use the `from:` prefix in the `den` tag:

```go
type ProductView struct {
    Name         string `json:"name"`
    CategoryName string `den:"from:category.name"`
}

var views []ProductView

err := den.NewQuery[Product](db).Project(ctx, &views)
```

!!! note
    Projections fetch full documents from the database and map the requested fields in Go memory. They reduce decode overhead for downstream code but do not reduce I/O at the SQL level.
