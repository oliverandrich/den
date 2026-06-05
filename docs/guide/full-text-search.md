# Full-Text Search

Den provides full-text search as an opt-in feature. Both backends use their native FTS engine, but the user-facing API is identical.

## Enabling FTS

Mark string fields for full-text indexing with the `den:"fts"` struct tag:

```go
type Article struct {
    document.Base
    Title string `json:"title" den:"fts"`
    Body  string `json:"body"  den:"fts"`
    Tags  string `json:"tags"`
}
```

When `den.Register()` processes this struct, it automatically creates the FTS infrastructure (virtual tables, generated columns, indexes, triggers) for every field tagged with `fts`.

!!! note
    If no fields carry `den:"fts"`, no FTS infrastructure is created. There is zero overhead for collections that do not use full-text search.

`den:"fts"` also works on fields of named struct fields and pointer-to-struct fields — see [Nested Field Indexes](documents.md#nested-field-indexes). Both backends index the dotted JSON path under the hood; SQLite's FTS5 column name has dots stripped, the path keeps them.

## Search API

```go
// Basic full-text search
articles, err := den.NewQuery[Article](db).Limit(20).Search(ctx, "golang embedded database")
```

`Search` is a terminal method -- it executes the query and returns `([]*T, error)` directly. Results are ranked by relevance (FTS5 `rank` on SQLite, `ts_rank` on PostgreSQL).

`Search` treats the term as **literal words ANDed together**: FTS5 operators and punctuation (`AND`/`OR`/`NEAR`, `col:term` scoping, prefix `*`, stray quotes) are neutralised, so raw user input from a search box or API is safe to pass straight through on both backends. A blank or whitespace-only term returns no rows. An all-blank term yields an empty result without touching the database.

### Raw query syntax

To use a backend's native FTS query language instead, call `SearchRaw`:

```go
articles, err := den.NewQuery[Article](db).SearchRaw(ctx, "golang AND embedded*")
```

`SearchRaw` passes the term straight through: an FTS5 query expression on SQLite (operators, column filters, and prefix `*` all honored -- and **raw user input unsafe**), and `plainto_tsquery` on PostgreSQL. To build a safe literal string yourself -- e.g. to compose a trusted operator with untrusted words -- use `den.LiteralFTS5(term)`, the same transform `Search` applies internally.

### Combined FTS + Conditions

Full-text search can be combined with regular where conditions and query options:

```go
articles, err := den.NewQuery[Article](db,
    where.Field("tags").Eq("tutorial"),
).Sort("_created_at", den.Desc).Search(ctx, "golang")
```

The FTS filter and the where conditions are applied together -- the database engine intersects both result sets using its query planner.

### Pagination

`Limit`, `Skip`, and cursor pagination (`After` / `Before`) all apply to `Search`. The default sort is by rank; pair cursor pagination with an explicit `Sort("_id", den.Asc)` so page boundaries stay stable across calls.

```go
first, _ := den.NewQuery[Article](db).Sort("_id", den.Asc).Limit(20).Search(ctx, "golang")
more, _ := den.NewQuery[Article](db).Sort("_id", den.Asc).After(first[len(first)-1].ID).
    Limit(20).Search(ctx, "golang")
```

Cursor + offset (`After`/`Before` combined with `Skip`) is rejected with `ErrIncompatiblePagination`, matching the rest of the QuerySet API.

## Backend Implementations

=== "SQLite"

    SQLite uses **FTS5 virtual tables** with content-sync triggers.

    **Infrastructure created during `Register()`:**

    ```sql
    -- FTS5 virtual table (external content, synced via triggers)
    CREATE VIRTUAL TABLE IF NOT EXISTS articles_fts USING fts5(
        title, body, content=articles, content_rowid=rowid
    );
    ```

    Den automatically creates triggers to keep the FTS index in sync on insert, update, and delete. Synchronization is fully atomic with the document write. When the FTS table is first created for an already-populated collection — such as after adding `den:"fts"` tags to an existing type — Den backfills the index from the existing rows in the same transaction.

    **Generated search query:**

    ```sql
    SELECT a.data FROM articles a
    JOIN articles_fts f ON a.rowid = f.rowid
    WHERE articles_fts MATCH ?
    ORDER BY rank
    LIMIT 20;
    ```

=== "PostgreSQL"

    PostgreSQL uses a **tsvector generated column** with a **GIN index**.

    **Infrastructure created during `Register()`:**

    ```sql
    -- Generated column combining all FTS-tagged fields
    ALTER TABLE articles ADD COLUMN _fts_vector tsvector
        GENERATED ALWAYS AS (
            to_tsvector('english',
                coalesce(data->>'title','') || ' ' ||
                coalesce(data->>'body','')
            )
        ) STORED;

    -- GIN index for fast lookups
    CREATE INDEX idx_articles_fts ON articles USING GIN(_fts_vector);
    ```

    **Generated search query:**

    ```sql
    SELECT data FROM articles
    WHERE _fts_vector @@ plainto_tsquery('english', $1)
    ORDER BY ts_rank(_fts_vector, plainto_tsquery('english', $1)) DESC
    LIMIT 20;
    ```

## Trade-offs

| Aspect | SQLite (FTS5) | PostgreSQL (tsvector) |
|--------|---------------|----------------------|
| Stemming | No language-aware stemming (requires ICU extension) | Full stemming support via language dictionaries |
| Ranking | BM25-based `rank` column | `ts_rank` with configurable weights |
| Phrase search | Supported via FTS5 syntax | Native phrase support via `phraseto_tsquery` |
| Index sync | Trigger-based (atomic) | Generated column (automatic) |
| Dependencies | None (built into SQLite) | None (built into PostgreSQL) |

!!! warning
    SQLite FTS5 does not support language-aware stemming out of the box. If your application requires stemming (e.g. matching "running" when searching for "run"), use the PostgreSQL backend or supply an external tokenizer.
