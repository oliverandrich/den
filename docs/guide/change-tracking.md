# Change Tracking

## Enabling Change Tracking

Embed `document.Tracked` alongside `document.Base`:

```go
import "github.com/oliverandrich/den/document"

type Product struct {
    document.Base
    document.Tracked
    Name  string  `json:"name"  den:"index"`
    Price float64 `json:"price" den:"index"`
}
```

Den automatically captures a byte-level snapshot of the serialized document after every load or write operation (`Insert`, `Update`, `FindByID`, query iteration).

## Detecting Changes

Use `den.IsChanged` to check if a document has been modified since it was last loaded or saved:

```go
p, _ := den.NewQuery[Product](db, where.Field("name").Eq("Widget")).First(ctx)

changed, _ := den.IsChanged(db, p) // false

p.Price = 29.99

changed, _ = den.IsChanged(db, p) // true
```

## Inspecting Changes

Use `den.GetChanges` to get a field-by-field diff:

```go
p.Price = 29.99

changes, _ := den.GetChanges(db, p)
// map[string]FieldChange{
//     "price": {Before: 19.99, After: 29.99},
// }
```

Only modified fields appear in the map. If nothing changed, the map is empty.

## Reverting Changes

Use `den.Revert` to restore a document to its last-saved state:

```go
p.Price = 29.99
p.Name = "New Name"

den.Revert(db, p)

// p.Price and p.Name are back to their original values
changed, _ = den.IsChanged(db, p) // false
```

`Revert` deserializes the stored snapshot back into the struct, undoing all local modifications. The name is deliberately not `Rollback` to avoid confusion with the backend transaction's `Rollback` method — `Revert` is a pure in-memory restore that has nothing to do with transactions.

!!! warning
    `den.Revert` returns `den.ErrNoSnapshot` if the document was never loaded from the database (e.g., a freshly constructed struct that has not been inserted or queried).

## Combining with Soft Delete

`Tracked` and `SoftDelete` are independent composable embeds:

```go
type AuditLog struct {
    document.Base
    document.SoftDelete
    document.Tracked
    Action string `json:"action"`
    Detail string `json:"detail"`
}
```

!!! tip
    See the [Documents](documents.md) guide for the full list of composable embeds and example compositions.

## How It Works

When a document implements the `Trackable` interface (which `document.Tracked` satisfies), Den stores a snapshot of the serialized JSON after every database operation:

- **`IsChanged`** re-encodes the current struct state and compares bytes against the snapshot
- **`GetChanges`** diffs the two JSON representations to produce a per-field change map
- **`Revert`** deserializes the stored snapshot back into the struct
