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

## Search API

```go
// Basic full-text search
articles, err := den.NewQuery[Article](ctx, db).Limit(20).Search("golang embedded database")
```

`Search` is a terminal method -- it executes the query and returns `([]*T, error)` directly. Results are ranked by relevance (FTS5 `rank` on SQLite, `ts_rank` on PostgreSQL).

### Combined FTS + Conditions

Full-text search can be combined with regular where conditions and query options:

```go
articles, err := den.NewQuery[Article](ctx, db,
    where.Field("tags").Eq("tutorial"),
).Sort("_created_at", den.Desc).Search("golang")
```

The FTS filter and the where conditions are applied together -- the database engine intersects both result sets using its query planner.

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

    Den automatically creates triggers to keep the FTS index in sync on insert, update, and delete. Synchronization is fully atomic with the document write.

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
