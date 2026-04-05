# Where Operators

Complete reference of all query operators in the `where` sub-package.

Import: `github.com/oliverandrich/den/where`

---

## Usage

```go
import "github.com/oliverandrich/den/where"

// Single condition
results, err := den.NewQuery[Product](ctx, db,
    where.Field("price").Gt(10.0),
).All()

// Multiple conditions (implicit AND)
results, err := den.NewQuery[Product](ctx, db,
    where.Field("price").Gte(10),
    where.Field("category").Eq("Electronics"),
).All()

// Nested field access via dot notation
results, err := den.NewQuery[Product](ctx, db,
    where.Field("address.city").Eq("Berlin"),
).All()
```

---

## Comparison Operators

| Operator | Example | Description | SQLite | PostgreSQL |
|---|---|---|---|---|
| `Eq` | `Field("name").Eq("Widget")` | Equal to value | `json_extract(data, '$.name') = ?` | `data->>'name' = $1` |
| `Ne` | `Field("status").Ne("deleted")` | Not equal to value | `json_extract(data, '$.status') != ?` | `data->>'status' != $1` |
| `Gt` | `Field("price").Gt(10)` | Greater than value | `json_extract(data, '$.price') > ?` | `(data->>'price')::float > $1` |
| `Gte` | `Field("price").Gte(10)` | Greater than or equal | `json_extract(data, '$.price') >= ?` | `(data->>'price')::float >= $1` |
| `Lt` | `Field("price").Lt(100)` | Less than value | `json_extract(data, '$.price') < ?` | `(data->>'price')::float < $1` |
| `Lte` | `Field("price").Lte(100)` | Less than or equal | `json_extract(data, '$.price') <= ?` | `(data->>'price')::float <= $1` |

## Null Operators

| Operator | Example | Description | SQLite | PostgreSQL |
|---|---|---|---|---|
| `IsNil` | `Field("read_at").IsNil()` | Field is null / not set | `json_extract(data, '$.read_at') IS NULL` | `data->>'read_at' IS NULL` |
| `IsNotNil` | `Field("read_at").IsNotNil()` | Field is not null | `json_extract(data, '$.read_at') IS NOT NULL` | `data->>'read_at' IS NOT NULL` |

## Set Operators

| Operator | Example | Description | SQLite | PostgreSQL |
|---|---|---|---|---|
| `In` | `Field("status").In("active", "pending")` | Value is one of the given values | `json_extract(data, '$.status') IN (?, ?)` | `data->>'status' = ANY($1)` |
| `NotIn` | `Field("status").NotIn("deleted", "banned")` | Value is not one of the given values | `json_extract(data, '$.status') NOT IN (?, ?)` | `data->>'status' != ALL($1)` |

## Array Operators

| Operator | Example | Description | SQLite | PostgreSQL |
|---|---|---|---|---|
| `Contains` | `Field("tags").Contains("golang")` | Array contains the value | `EXISTS (SELECT 1 FROM json_each(...) WHERE value = ?)` | `data->'tags' @> '["golang"]'::jsonb` |
| `ContainsAny` | `Field("tags").ContainsAny("go", "rust")` | Array contains any of the values | Multiple `EXISTS` with `OR` | `data->'tags' ?| array[...]` |
| `ContainsAll` | `Field("tags").ContainsAll("go", "web")` | Array contains all of the values | Multiple `EXISTS` with `AND` | `data->'tags' @> '[...]'::jsonb` |

## Map / Object Operators

| Operator | Example | Description | SQLite | PostgreSQL |
|---|---|---|---|---|
| `HasKey` | `Field("metadata").HasKey("color")` | Map/object contains the given key | `json_extract(data, '$.metadata.color') IS NOT NULL` | `data->'metadata' ? 'color'` |

## String Operators

| Operator | Example | Description | SQLite | PostgreSQL |
|---|---|---|---|---|
| `StringContains` | `Field("name").StringContains("alpha")` | Field contains the substring | `json_extract(data, '$.name') LIKE '%alpha%' ESCAPE '\'` | `data->>'name' ILIKE '%alpha%'` |
| `StartsWith` | `Field("name").StartsWith("Al")` | Field starts with the prefix | `json_extract(data, '$.name') LIKE 'Al%' ESCAPE '\'` | `data->>'name' ILIKE 'Al%'` |
| `EndsWith` | `Field("name").EndsWith("ta")` | Field ends with the suffix | `json_extract(data, '$.name') LIKE '%ta' ESCAPE '\'` | `data->>'name' ILIKE '%ta'` |

> **Note:** String operators automatically escape special SQL characters (`%`, `_`, `\`) in the search term.

## Pattern Matching

| Operator | Example | Description | SQLite | PostgreSQL |
|---|---|---|---|---|
| `RegExp` | `Field("name").RegExp("^W.+t$")` | Match field against a regular expression | `json_extract(data, '$.name') REGEXP ?` | `data->>'name' ~ $1` |

---

## Logical Operators

Logical operators compose multiple conditions.

| Operator | Example | Description |
|---|---|---|
| `And` | `where.And(cond1, cond2, ...)` | All conditions must match |
| `Or` | `where.Or(cond1, cond2, ...)` | At least one condition must match |
| `Not` | `where.Not(cond)` | Negate a condition |

### Examples

```go
// AND: price between 10 and 100
results, err := den.NewQuery[Product](ctx, db,
    where.And(
        where.Field("price").Gt(10),
        where.Field("price").Lt(100),
    ),
).All()

// OR: active or featured
results, err := den.NewQuery[Product](ctx, db,
    where.Or(
        where.Field("status").Eq("active"),
        where.Field("featured").Eq(true),
    ),
).All()

// NOT: exclude deleted
results, err := den.NewQuery[Product](ctx, db,
    where.Not(where.Field("deleted").Eq(true)),
).All()

// Combined
results, err := den.NewQuery[Product](ctx, db,
    where.And(
        where.Field("price").Gte(10),
        where.Or(
            where.Field("category").Eq("Electronics"),
            where.Field("featured").Eq(true),
        ),
    ),
).All()
```

---

## Nested Fields

Use dot notation to access nested fields in embedded objects:

```go
// Object field
where.Field("address.city").Eq("Berlin")

// Deeply nested
where.Field("category.parent.name").Eq("Root")

// Array index
where.Field("tags.0").Eq("featured")
```
