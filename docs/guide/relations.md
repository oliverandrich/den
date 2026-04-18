# Relations

Den models relations via the generic `Link[T]` type, inspired by Beanie's `Link[Document]`. Links store only an ID in the database -- the referenced document is fetched on demand or eagerly during queries.

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
houses, err := den.NewQuery[House](ctx, db,
    where.Field("name").Eq("Lakehouse"),
).All()

houses[0].Door.ID        // "01HQ4..."
houses[0].Door.Value     // nil
houses[0].Door.IsLoaded() // false
```

### Eager (WithFetchLinks)

Resolve all links during the query. Additional lookups happen within the same transaction for read consistency:

```go
houses, err := den.NewQuery[House](ctx, db,
    where.Field("name").Eq("Lakehouse"),
).WithFetchLinks().All()

houses[0].Door.Value     // *Door{Height: 200, Width: 90}
houses[0].Door.IsLoaded() // true
houses[0].Windows[0].Value // *Window{X: 100, Y: 50}
```

=== "SQLite"

    Each link resolution is a direct in-process lookup -- no network overhead.

=== "PostgreSQL"

    Link fetches are batched into a single query where possible, minimizing round trips.

### On-Demand (FetchLink / FetchAllLinks)

Fetch individual link fields or all link fields after the initial query:

```go
house, _ := den.NewQuery[House](ctx, db,
    where.Field("name").Eq("Lakehouse"),
).First()

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

Cascades deletion to all linked documents:

```go
err := den.Delete(ctx, db, house, den.WithLinkRule(den.LinkDelete))
// Deletes the House, its Door, and all its Windows
```

!!! warning
    `LinkDelete` cascades recursively. If a Door has its own links, those are also deleted. Be mindful of the nesting depth and your document graph when using cascade delete.

## BackLinks -- Reverse Queries

Find all documents of a given type that reference a specific document via a link field:

```go
// Find all Houses that reference a specific Door
houses, err := den.BackLinks[House](ctx, db, "door", doorID)
```

This is useful for answering "who links to this document?" without maintaining an explicit reverse reference field.

## Nesting Depth

When documents link to documents that themselves contain links, eager fetching can cause deep recursion or infinite loops with circular references. Den limits the maximum nesting depth.

### Default

The default maximum nesting depth is **3 levels**.

### Per-Query Override

Override the depth for a specific query:

```go
houses, err := den.NewQuery[House](ctx, db).
    WithFetchLinks().WithNestingDepth(1).All()
```

!!! note
    When the depth counter reaches zero, remaining `Link[T]` fields are left unresolved -- they contain the ID but `Value` is `nil` and `IsLoaded()` returns `false`. This prevents infinite loops and bounds resource consumption.

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
    found, _ := den.NewQuery[House](ctx, db,
        where.Field("name").Eq("Lakehouse"),
    ).WithFetchLinks().First()

    fmt.Println(found.Door.Value.Height)      // 200
    fmt.Println(found.Windows[0].Value.X)      // 100

    // Back-links: find houses referencing this door
    houses, _ := den.BackLinks[House](ctx, db, "door", found.Door.ID)
    fmt.Println(len(houses)) // 1

    // Cascade delete
    den.Delete(ctx, db, found, den.WithLinkRule(den.LinkDelete))
}
```
