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

!!! note "Link equality compares the ID, not the Value"
    Because the on-disk representation is just the ID, two `Link[T]` values referring to the same target document compare equal by their `ID` field — regardless of whether one was freshly constructed via `NewLink(&doc)` (Loaded=true, Value populated) and the other was decoded from storage (Loaded=false, Value=nil). Code that needs to test "do these two links point at the same row" should compare `a.ID == b.ID`. Comparing `a == b` directly will return `false` for two valid links to the same target whenever their `Loaded`/`Value` differ.

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

### Lazy (Default for untagged fields)

Untagged `Link[T]` fields contain only the target ID after a read; no
additional query is performed unless the caller asks for one. Fields
tagged with [`den:"eager"`](#schema-level-eager-deneager) opt out of
lazy by default — see below.

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

### Schema-level eager (`den:"eager"`)

Tag a `Link[T]` or `[]Link[T]` field with `den:"eager"` and Den hydrates it on every read, no `WithFetchLinks()` call needed. This is the Django `select_related` analogue applied to the schema instead of the queryset:

```go
type Person struct {
    document.Base
    Name string `json:"name"`
}

type House struct {
    document.Base
    Name    string         `json:"name"`
    Door    Link[Door]     `json:"door"  den:"eager"` // always hydrated
    Owner   Link[Person]   `json:"owner"`             // stays lazy
    Windows []Link[Window] `json:"windows"`           // stays lazy
}

houses, _ := den.NewQuery[House](db).All(ctx)
houses[0].Door.IsLoaded()      // true — eager
houses[0].Owner.IsLoaded()     // false — untagged
houses[0].Windows[0].IsLoaded() // false — untagged
```

Three modes interact with the tag:

| Modifier | Behaviour |
|---|---|
| (none, default) | Hydrate `den:"eager"` fields, leave the rest lazy |
| `WithFetchLinks()` | Hydrate every link, eager or not |
| `WithoutFetchLinks()` | Hydrate nothing, even eager fields — bulk-export escape hatch |

The default-mode resolver still uses the same batched `WHERE _id IN (…)` path on `.All` / `.AllWithCount` / `.Search`, so eager fields cost one extra query per target type per page rather than N+1.

The CRUD-style read APIs honor the same tag without a QuerySet: `FindByID`, `FindByIDs`, `Refresh`, `BackLinks`, `BackLinksField`, `FindOneAndUpdate`, `FindOneAndUpsert`, and `FindOrCreate` all hydrate eager-tagged fields by default. The `den.WithoutFetchLinks()` CRUDOption is the opt-out, mirroring the QuerySet modifier.

```go
// FindByID honors den:"eager" — Door is hydrated, Owner is not.
h, _ := den.FindByID[House](ctx, db, houseID)

// Suppress eager hydration on this one read.
h, _ = den.FindByID[House](ctx, db, houseID, den.WithoutFetchLinks())
```

!!! warning "`Iter` is N+1 for eager fields"
    `.Iter(ctx)` honors the tag too, but resolves per row (streaming can't batch). An eager field on an `Iter` consumer is N+1 by construction — prefer `.All` when you actually want the eager hit, or call `WithoutFetchLinks()` on the `Iter` chain for genuine bulk scans where even eager would be wasted. `FindByID` and the other single-doc read APIs share this single-level contract: they hydrate the direct eager fields but do not recurse into the loaded targets' own eager links. Use `NewQuery[T](db, where.Field(den.FieldID).Eq(id)).All(ctx)` when you need transitive hydration of a single doc.

`FetchLink` / `FetchAllLinks` always hydrate everything passed to them — they're explicit user calls, the `eager` tag does not constrain them.

!!! note "Tag validity is enforced at Register"
    `den:"eager"` is only valid on `Link[T]` or `[]Link[T]` fields. Placing it on any other field type causes `Register` to return `ErrValidation`, mirroring the existing register-time guards for `index`, `unique`, and `fts` on incompatible field types.

#### Eager + soft-delete

A soft-deleted target is **not** filtered out of eager hydration. The
link resolver fetches by ID directly (single-doc `Get` or batched
`WHERE _id IN (...)`), and that path does not apply the soft-delete
filter — it matches `FindByID`'s own behavior of returning a doc by
ID regardless of its `DeletedAt`. The loaded `Value` is the
soft-deleted record; callers can detect this via
`link.Value.IsDeleted()` and decide what to do.

```go
target := &Account{Name: "x"}
den.Insert(ctx, db, target)

holder := &Holder{Account: den.NewLink(target)}
den.Insert(ctx, db, holder)

den.Delete(ctx, db, target) // soft-delete

got, _ := den.FindByID[Holder](ctx, db, holder.ID)
got.Account.IsLoaded()     // true — eager hydration is soft-delete-blind
got.Account.Value.IsDeleted() // true — caller can detect
```

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

Read the parameter order as a sentence: **"Find `[House]`s where field `door` equals `doorID`."** The type parameter is the *holding* type (the side that has the `Link` field); the string is the JSON tag name of that link field; the third argument is the target ID being pointed at. Renaming the JSON tag on `House.Door` silently breaks every `BackLinks` call against this collection — keep the tag stable, or use the typed variant below.

This is useful for answering "who links to this document?" without maintaining an explicit reverse reference field.

### Typed variant: `BackLinksField[H, T]`

When the holder has exactly one `Link[T]` field for the target type, the typed variant skips the string field name entirely:

```go
houses, err := den.BackLinksField[House, Door](ctx, db, doorID)
```

The framework walks `House`'s fields once, finds the unique `Link[Door]` field, and uses its JSON tag for the underlying query. Renaming the JSON tag is caught at the next call instead of silently returning wrong results. Two type parameters are required (Go can't infer them from the `targetID string`), but the call is otherwise identical.

The string-based form stays for two cases the typed lookup deliberately rejects with a clear error:

- **Multiple `Link[T]` fields on the holder** (e.g. `FrontDoor` and `BackDoor` both `Link[Door]`) — disambiguate by passing the explicit JSON tag to `BackLinks`.
- **Slice-link fields** (`[]Link[T]`) — the underlying query uses `Eq`, which doesn't match against array contents. Use a manual `where.Field("...").Contains(targetID)` query for slice-link backlinks.

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
    Door    Link[Door]     `json:"door" den:"eager"` // schema-level eager
    Windows []Link[Window] `json:"windows"`           // stays lazy
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

    // Schema-level eager: Door is hydrated automatically — no
    // WithFetchLinks() needed. Windows stays lazy because it's
    // not tagged eager; call .WithFetchLinks() when you want it too.
    found, _ := den.NewQuery[House](db,
        where.Field("name").Eq("Lakehouse"),
    ).First(ctx)

    fmt.Println(found.Door.Value.Height)         // 200 — eager hit
    fmt.Println(found.Windows[0].IsLoaded())      // false — lazy

    // Back-links: find houses referencing this door
    houses, _ := den.BackLinks[House](ctx, db, "door", found.Door.ID)
    fmt.Println(len(houses)) // 1

    // Cascade delete
    den.Delete(ctx, db, found, den.WithLinkRule(den.LinkDelete))
}
```
