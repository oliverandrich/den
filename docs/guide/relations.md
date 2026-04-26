# Relations

Den models relations via the generic `Link[T]` type, inspired by Beanie's `Link[Document]`. Links store only an ID in the database -- the referenced document is fetched on demand or eagerly during queries.

## Embed or Link?

Den uses the word *embed* in two senses — keep them apart:

- **Go-level embedding** (`document.Base`, `document.SoftDelete`, ...) is struct composition, a way to mix feature fields and methods into a document type.
- **Relational embedding** is a modelling decision: nest a sub-struct *inside* a parent document's JSONB, or give it its own collection and reference it by ID with `Link[T]`. This section is about the second kind.

### When to embed, when to link

| Question | Embed (nested struct) | Link (`Link[T]`) |
|---|---|---|
| Does it have its own identity you query or update independently? | No | Yes |
| Is it shared between multiple parents? | No | Yes |
| Do you load the parent without needing the child? | Rarely | Often |
| Does it have its own lifecycle or outlive the parent? | No | Yes |
| Is it a small value object (address, money, flags)? | Yes | No |

**Rule of thumb:** embed *value objects* (always read together with the parent, no independent identity, bound to the parent's lifecycle). Link *entities* (own ID, queried on their own, possibly shared, possibly outliving the parent).

### Example

```go
// Address is a value object — no identity, always read with the Order,
// never queried on its own. Embed it as a nested struct.
type Address struct {
    Street  string `json:"street"`
    City    string `json:"city"`
    Country string `json:"country"`
}

type Order struct {
    document.Base
    Ship   Address        `json:"ship"`
    Bill   Address        `json:"bill"`
    Author den.Link[User] `json:"author"` // User is an entity — own collection, shared
}
```

Embedded `Address` values live inside the order's JSONB. The `Author` link stores only the user's ID; `den.FetchLink` or `WithFetchLinks()` resolves it when needed.

## Link Type

```go
type Link[T any] struct {
    ID     string // persisted to storage
    Value  *T     // resolved document (nil until fetched)
    Loaded bool   // internal
}

// NewLink creates a Link from an existing document, extracting its ID.
func NewLink[T any](doc *T) Link[T]

// IsLoaded reports whether the linked document has been fetched.
func (l Link[T]) IsLoaded() bool
```

## Relation Patterns

### One-to-One

```go
type House struct {
    document.Base
    Name string    `json:"name"`
    Door Link[Door] `json:"door"`
}
```

### One-to-Many

```go
type House struct {
    document.Base
    Name    string         `json:"name"`
    Door    Link[Door]     `json:"door"`
    Windows []Link[Window] `json:"windows"`
}
```

!!! note
    Many-to-many relations are not directly supported. Model them with an intermediary document that holds links to both sides.

## Serialization

When a document is written to storage, `Link[T]` fields serialize to their ID only. The `Value` and `Loaded` fields are transient:

```go
// Stored JSON for a House document:
{
    "_id": "01HQ3...",
    "name": "Lakehouse",
    "door": "01HQ4...",
    "windows": ["01HQ5...", "01HQ6..."]
}
```

## Creating Links

Use `NewLink` to create a link from an existing document:

```go
door := &Door{Height: 200, Width: 90}
den.Insert(ctx, db, door)

house := &House{
    Name: "Lakehouse",
    Door: den.NewLink(door),
    Windows: []Link[Window]{
        den.NewLink(&Window{X: 100, Y: 50}),
        den.NewLink(&Window{X: 120, Y: 60}),
    },
}
```

## Fetch Modes

### Lazy (Default)

Links contain only IDs. No additional queries are performed:

```go
houses, err := den.NewQuery[House](db,
    where.Field("name").Eq("Lakehouse"),
).All(ctx)

houses[0].Door.ID        // "01HQ4..."
houses[0].Door.Value     // nil
houses[0].Door.IsLoaded() // false
```

### Eager (WithFetchLinks)

Resolve all links during the query:

```go
houses, err := den.NewQuery[House](db,
    where.Field("name").Eq("Lakehouse"),
).WithFetchLinks().All(ctx)

houses[0].Door.Value     // *Door{Height: 200, Width: 90}
houses[0].Door.IsLoaded() // true
houses[0].Windows[0].Value // *Window{X: 100, Y: 50}
```

`.All(ctx)` drains the result first, then runs **one batched `WHERE _id IN (…)` query per target type per nesting level**. IDs are deduplicated, so a hot target referenced by many parents is fetched once and the decoded pointer is shared across all matching slots — `houses[0].Door.Value == houses[1].Door.Value` when they point at the same ID. `AllWithCount` and `Search` use the same batched path.

`.WithFetchLinks().Iter(ctx)` resolves per row instead, because batching would require buffering the whole result set, which defeats `Iter`'s streaming contract. Use `.Iter` when the result set is too large to materialize; otherwise prefer `.All` so you get the batched resolver.

=== "SQLite"

    Each link resolution is a direct in-process lookup -- no network overhead. Batching still cuts allocations and decode work.

=== "PostgreSQL"

    Each batch collapses N per-row `SELECT` round-trips into one `WHERE _id IN (…)`. On a 20-parent fixture with one shared link, this takes `WithFetchLinks` from ~1.6 ms down to ~630 µs.

### On-Demand (FetchLink / FetchAllLinks)

Fetch individual link fields or all link fields after the initial query:

```go
house, _ := den.NewQuery[House](db,
    where.Field("name").Eq("Lakehouse"),
).First(ctx)

// Fetch a single link field
err := den.FetchLink(ctx, db, house, "door")
house.Door.IsLoaded() // true

// Fetch all link fields at once
err := den.FetchAllLinks(ctx, db, house)
```

## Write Rules

Control whether linked documents are automatically saved when the parent is inserted or updated.

### LinkIgnore (Default)

Only the root document is written. Linked documents must already exist in the database:

```go
err := den.Insert(ctx, db, house, den.WithLinkRule(den.LinkIgnore))
// Saves only the House -- Door and Windows must already be inserted
```

### LinkWrite

Cascades write operations to all linked documents. New linked documents are inserted, existing ones are replaced:

```go
house := &House{
    Name: "Lakehouse",
    Door: den.NewLink(&Door{Height: 200, Width: 90}),
    Windows: []Link[Window]{
        den.NewLink(&Window{X: 100, Y: 50}),
    },
}

err := den.Insert(ctx, db, house, den.WithLinkRule(den.LinkWrite))
// Saves House, Door, and all Windows in one operation
```

This also works with `Update`:

```go
house.Door = den.NewLink(&Door{Height: 210, Width: 95})
err := den.Update(ctx, db, house, den.WithLinkRule(den.LinkWrite))
// Updates the House and inserts/replaces the new Door
```

## Delete Rules

Control whether linked documents are deleted when the parent is deleted.

### LinkIgnore (Default)

Linked documents are kept when the parent is deleted:

```go
err := den.Delete(ctx, db, house, den.WithLinkRule(den.LinkIgnore))
// Deletes only the House -- Door and Windows remain
```

### LinkDelete

Cascades deletion to the immediate linked documents:

```go
err := den.Delete(ctx, db, house, den.WithLinkRule(den.LinkDelete))
// Deletes the House, its Door, and its Windows.
// If Door or Window has its own links, those are NOT touched.
```

!!! note
    `LinkDelete` is single-level: only the immediate link targets are deleted. Linked-document graphs beyond one level stay intact (orphan references point at deleted parents). If you need transitive cleanup, call `Delete(..., WithLinkRule(LinkDelete))` explicitly on each node, or walk the graph yourself — the framework does not recurse. This design keeps one mis-configured delete from wiping an unbounded subgraph.

## BackLinks -- Reverse Queries

Find all documents of a given type that reference a specific document via a link field:

```go
// Find all Houses that reference a specific Door
houses, err := den.BackLinks[House](ctx, db, "door", doorID)
```

Read the parameter order as a sentence: **"Find `[House]`s where field `door` equals `doorID`."** The type parameter is the *holding* type (the side that has the `Link` field); the string is the JSON tag name of that link field; the third argument is the target ID being pointed at. Renaming the JSON tag on `House.Door` silently breaks every `BackLinks` call against this collection — keep the tag stable, or define a constant for the field name.

This is useful for answering "who links to this document?" without maintaining an explicit reverse reference field.

## Nesting Depth

When documents link to documents that themselves contain links, eager fetching can cause deep recursion or infinite loops with circular references. Den limits the maximum nesting depth.

### Default

The default maximum nesting depth is **3 levels**.

### Per-Query Override

Override the depth for a specific query:

```go
houses, err := den.NewQuery[House](db).
    WithFetchLinks().WithNestingDepth(2).All(ctx)
```

With `WithNestingDepth(2)` against `Root → Mid → Leaf`, the batched resolver runs one query per target type for `Mid`, then one query per target type for `Leaf` on the loaded `Mid` set — two round-trips total, regardless of how many parents. When the depth counter reaches zero, remaining `Link[T]` fields are left unresolved (`Value` stays `nil`, `IsLoaded()` returns `false`).

!!! warning "Recursion requires `.All` (or `.AllWithCount` / `.Search`)"
    Nested resolution only descends in the batched paths. Streaming `.Iter(ctx)` resolves the direct level of every yielded document but does not recurse into loaded targets — it would otherwise have to buffer results to run a coherent batch.

## Complete Example

A full House/Door/Window example demonstrating links, cascade write, eager fetch, and back-links:

```go
type Door struct {
    document.Base
    Height int `json:"height"`
    Width  int `json:"width"`
}

type Window struct {
    document.Base
    X int `json:"x"`
    Y int `json:"y"`
}

type House struct {
    document.Base
    Name    string         `json:"name"`
    Door    Link[Door]     `json:"door"`
    Windows []Link[Window] `json:"windows"`
}

func main() {
    db, _ := den.OpenURL(ctx, "sqlite:///data.db")
    defer db.Close()

    ctx := context.Background()
    den.Register(ctx, db, &House{}, &Door{}, &Window{})

    // Create and insert with cascade
    house := &House{
        Name: "Lakehouse",
        Door: den.NewLink(&Door{Height: 200, Width: 90}),
        Windows: []Link[Window]{
            den.NewLink(&Window{X: 100, Y: 50}),
            den.NewLink(&Window{X: 120, Y: 60}),
        },
    }
    den.Insert(ctx, db, house, den.WithLinkRule(den.LinkWrite))

    // Query with eager fetch
    found, _ := den.NewQuery[House](db,
        where.Field("name").Eq("Lakehouse"),
    ).WithFetchLinks().First(ctx)

    fmt.Println(found.Door.Value.Height)      // 200
    fmt.Println(found.Windows[0].Value.X)      // 100

    // Back-links: find houses referencing this door
    houses, _ := den.BackLinks[House](ctx, db, "door", found.Door.ID)
    fmt.Println(len(houses)) // 1

    // Cascade delete
    den.Delete(ctx, db, found, den.WithLinkRule(den.LinkDelete))
}
```
