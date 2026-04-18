# Documents

## Base Types

Every Den document embeds `document.Base` — the required anchor that carries ID, timestamps, and the revision token. The two orthogonal features (soft delete and change tracking) are available as separate composable embeds; combine whichever you need.

| Embed | Purpose |
|---|---|
| `document.Base` | Required. Provides `ID`, `CreatedAt`, `UpdatedAt`, `Rev` |
| `document.SoftDelete` | Opt-in. Adds `DeletedAt *time.Time` and `IsDeleted()` so `Delete` soft-deletes instead of physically removing |
| `document.Tracked` | Opt-in. Adds the byte-snapshot machinery so `IsChanged`, `GetChanges`, and `Revert` work |

```go
package document

type Base struct {
    ID        string    `json:"_id"`
    CreatedAt time.Time `json:"_created_at"`
    UpdatedAt time.Time `json:"_updated_at"`
    Rev       string    `json:"_rev,omitempty"` // populated when UseRevision is enabled
}

type SoftDelete struct {
    DeletedAt *time.Time `json:"_deleted_at,omitempty"`
}

type Tracked struct {
    snapshot []byte // not serialized
}
```

Typical compositions:

```go
type Product struct {
    document.Base
    Name string `json:"name"`
}

type Article struct {
    document.Base
    document.SoftDelete
    Title string `json:"title"`
}

type User struct {
    document.Base
    document.Tracked
    Email string `json:"email"`
}

type AuditLog struct {
    document.Base
    document.SoftDelete
    document.Tracked
    Action string `json:"action"`
}
```

Den detects both features **structurally**: soft-delete by the presence of the `_deleted_at` JSON field, change tracking by the `Trackable` interface. Any type that carries the right fields / methods participates, even without these specific embeds.

## Struct Tag Syntax

Den uses two struct tags with distinct responsibilities:

- **`json`** -- Sets the serialized field name (the key stored in JSONB). Standard Go `encoding/json` rules apply.
- **`den`** -- Carries Den-specific metadata only. No field name, just options.

Available `den` tag options:

| Option | Effect |
|---|---|
| `index` | Creates a secondary index on this field |
| `unique` | Creates a unique index on this field |
| `fts` | Includes this field in full-text search |
| `omitempty` | Omits the field from storage when it has a zero value |
| `unique_together:group` | Groups fields into a composite unique index by group name |
| `index_together:group` | Groups fields into a composite non-unique index by group name |

```go
type Product struct {
    document.Base
    Name  string   `json:"name"  den:"index"`
    SKU   string   `json:"sku"   den:"unique"`
    Price float64  `json:"price" den:"index"`
    Body  string   `json:"body"  den:"fts"`
    Tags  []string `json:"tags"  den:"index,omitempty"`
}
```

!!! note
    The `json` tag controls serialization -- it determines the key name in the stored JSONB document. The `den` tag never contains a field name; it only carries options.

!!! warning "Field name validation"
    `Register` rejects JSON field names that do not match `^[A-Za-z_][A-Za-z0-9_]*$` — identifiers only, no spaces, dots, quotes, or punctuation. The error wraps `den.ErrValidation`. This protects the JSONB path expressions used by Den's SQL builder against tag-sourced injection. In practice you will hit it only when migrating data from a system that used unusual field names.

## Collection Naming

Den derives collection names automatically: lowercase struct name, no pluralization.

| Struct | Collection Name |
|---|---|
| `Product` | `product` |
| `Category` | `category` |
| `AuditLog` | `auditlog` |

Override the default by implementing `DenSettings()`:

```go
func (p Product) DenSettings() den.Settings {
    return den.Settings{
        CollectionName: "products",
    }
}
```

## ID Generation

Den uses [ULID](https://github.com/oklog/ulid) for document IDs -- lexicographically sortable, timestamp-ordered, 26 characters.

```go
p := &Product{Name: "Widget", Price: 9.99}
den.Insert(ctx, db, p)
fmt.Println(p.ID) // "01HQ3K8V2X..."
```

ULIDs are preferred over UUIDs because they sort chronologically, which benefits B-tree indexes in both SQLite and PostgreSQL -- improving write and scan performance.

You can set an ID manually before insert. If `ID` is empty, Den generates a ULID automatically:

```go
p := &Product{Name: "Widget"}
p.ID = "my-custom-id"
den.Insert(ctx, db, p) // uses "my-custom-id"
```

## DenSettings Interface

Implement `DenSettings()` on your document struct to configure collection-level behavior:

```go
type DenSettable interface {
    DenSettings() den.Settings
}

type Settings struct {
    CollectionName       string            // override auto-derived name
    OmitEmpty            bool              // omit zero-value fields by default
    UseRevision          bool              // enable optimistic concurrency control
    NestingDepthPerField map[string]int    // per-field nesting depth overrides
    Indexes              []IndexDefinition // compound indexes
}
```

Example with multiple settings:

```go
func (p Product) DenSettings() den.Settings {
    return den.Settings{
        CollectionName: "products",
        UseRevision:    true,
        OmitEmpty:      true,
        Indexes: []den.IndexDefinition{
            {Name: "idx_category_price", Fields: []string{"category", "price"}},
        },
    }
}
```

## Index Definitions

### Via Struct Tags

Single-field indexes are defined directly on the struct:

```go
type Product struct {
    document.Base
    Name  string  `json:"name"  den:"index"`   // secondary index
    SKU   string  `json:"sku"   den:"unique"`  // unique index
    Price float64 `json:"price" den:"index"`   // secondary index
}
```

=== "SQLite"

    ```sql
    CREATE INDEX IF NOT EXISTS idx_product_name
        ON product(json_extract(data, '$.name'));

    CREATE UNIQUE INDEX IF NOT EXISTS idx_product_sku
        ON product(json_extract(data, '$.sku'));
    ```

=== "PostgreSQL"

    ```sql
    CREATE INDEX IF NOT EXISTS idx_product_name
        ON product(((data->>'name')));

    CREATE UNIQUE INDEX IF NOT EXISTS idx_product_sku
        ON product(((data->>'sku')));
    ```

### Via Struct Tags (Compound Indexes)

For multi-field indexes, use `unique_together` or `index_together` with a shared group name. Fields with the same group name are combined into a single composite index:

```go
type Entry struct {
    document.Base
    Feed string `json:"feed" den:"unique_together:feed_guid"`
    GUID string `json:"guid" den:"unique_together:feed_guid"`
    Body string `json:"body"`
}
```

This creates a composite unique index on `(feed, guid)` -- the combination must be unique, but individual values can repeat. The group name (`feed_guid`) becomes part of the index name: `idx_entry_feed_guid`.

For non-unique composite indexes, use `index_together`:

```go
type Event struct {
    document.Base
    UserID string `json:"user_id" den:"index_together:user_date"`
    Date   string `json:"date"    den:"index_together:user_date"`
}
```

=== "SQLite"

    ```sql
    CREATE UNIQUE INDEX IF NOT EXISTS idx_entry_feed_guid
        ON entry(json_extract(data, '$.feed'), json_extract(data, '$.guid'))
        WHERE json_extract(data, '$.feed') IS NOT NULL
          AND json_extract(data, '$.guid') IS NOT NULL;
    ```

=== "PostgreSQL"

    ```sql
    CREATE UNIQUE INDEX IF NOT EXISTS idx_entry_feed_guid
        ON entry((data->>'feed'), (data->>'guid'))
        WHERE data->>'feed' IS NOT NULL
          AND data->>'guid' IS NOT NULL;
    ```

### Via Settings (Compound Indexes)

Alternatively, use `DenSettings()` for programmatic index definitions:

```go
func (p Product) DenSettings() den.Settings {
    return den.Settings{
        Indexes: []den.IndexDefinition{
            {Name: "idx_category_price", Fields: []string{"category", "price"}},
            {Name: "idx_tenant_sku", Fields: []string{"tenant_id", "sku"}, Unique: true},
        },
    }
}
```

!!! tip
    Prefer `unique_together`/`index_together` struct tags for most cases -- they're declarative and co-located with the fields. Use `DenSettings().Indexes` when you need full control over index names or when the index definition doesn't map cleanly to struct fields.

### Index Creation Behavior

=== "SQLite"

    Indexes are created with `CREATE INDEX IF NOT EXISTS` as part of `Register()`. SQLite is fast enough in-process that blocking is rarely a concern.

=== "PostgreSQL"

    Indexes are created with `CREATE INDEX CONCURRENTLY IF NOT EXISTS`. Concurrent writes on the collection are not blocked during index creation, which matters on large tables.

    If a previous `CONCURRENTLY` run was interrupted (process killed, query cancelled), PostgreSQL may leave behind an invalid index. Den detects invalid indexes via `pg_index.indisvalid` on the next `Register()` call, drops them, and recreates them cleanly — no manual intervention required.

### Dropping Stale Indexes

`Register()` is additive: it creates new indexes but never removes obsolete ones. When you remove a `den:"index"` or `den:"unique"` tag, or rename a field, the old index stays in the database.

To clean up, call `den.DropStaleIndexes()`:

```go
result, err := den.DropStaleIndexes(ctx, db)
if err != nil {
    return err
}
log.Printf("dropped %d stale indexes, kept %d", len(result.Dropped), len(result.Kept))
```

To preview what would be dropped without actually dropping anything, pass `den.DryRun()`:

```go
result, _ := den.DropStaleIndexes(ctx, db, den.DryRun())
for _, idx := range result.Dropped {
    log.Printf("would drop: %s.%s (fields=%v)", idx.Collection, idx.Name, idx.Fields)
}
```

Den tracks indexes it created in a private `_den_indexes` metadata table, so this operation only considers indexes Den knows about. Managed indexes (the PostgreSQL GIN index, FTS triggers and auxiliary tables, application-created indexes that Den did not create) are never touched.

!!! tip
    Typical usage is from a migration or deployment script, not on every startup. Running `DropStaleIndexes` unconditionally on every process start is safe but unnecessary — it only does work when the struct has actually changed.

## Nullable Unique Constraints

When a pointer field is tagged with `den:"unique"`, Den creates a partial unique index. Uniqueness is only enforced for non-nil values -- multiple documents can have `nil` for that field.

```go
type User struct {
    document.Base
    Username string  `json:"username" den:"unique"`         // always required, always unique
    Email    *string `json:"email,omitempty" den:"unique"`  // optional, unique when set
}

// Both users have nil Email -- no conflict:
den.Insert(ctx, db, &User{Username: "alice"})  // Email: nil
den.Insert(ctx, db, &User{Username: "bob"})    // Email: nil

// But duplicate non-nil values are rejected:
den.Insert(ctx, db, &User{Username: "carol", Email: ptr("carol@example.com")}) // ok
den.Insert(ctx, db, &User{Username: "dave",  Email: ptr("carol@example.com")}) // ErrDuplicate
```

=== "SQLite"

    ```sql
    CREATE UNIQUE INDEX IF NOT EXISTS idx_user_email
        ON user(json_extract(data, '$.email'))
        WHERE json_extract(data, '$.email') IS NOT NULL;
    ```

=== "PostgreSQL"

    ```sql
    CREATE UNIQUE INDEX IF NOT EXISTS idx_user_email
        ON "user"(((data->>'email')))
        WHERE (data->>'email') IS NOT NULL;
    ```

!!! warning
    Nullable unique constraints require pointer fields (`*string`, `*int`, etc.). Value-type fields with `den:"unique"` always enforce uniqueness, including for zero values.
