# Recipes

Patterns most apps need eventually. Each recipe is a few lines of code with the surrounding context — meant for copy-paste, not exhaustive coverage. For the underlying primitives see the linked guide pages.

---

## Update one field on one known document

When you know the ID and only want to flip a single field, route through `FindOneAndUpdate` so the update is atomic and avoids the read-modify-write round-trip:

```go
done, err := den.FindOneAndUpdate[Todo](ctx, db,
    den.SetFields{"done": true},
    []where.Condition{where.Field("_id").Eq(todoID)},
)
```

Returns `ErrNotFound` if the document was deleted between your read and this call. If the document carries `document.SoftDelete` and you want to flip a field on a soft-deleted doc, add `den.IncludeDeleted()`.

→ [`FindOneAndUpdate`](crud.md#findoneandupdate)

---

## Find or create with defaults

Atomic find-or-create using `FindOneAndUpsert`. The `defaults` template is used only on miss; `fields` is applied on both branches (pass `den.SetFields{}` if you want existing rows untouched):

```go
user, inserted, err := den.FindOneAndUpsert[User](ctx, db,
    &User{Email: "x@y.z", LoginCount: 0},   // defaults — applied on miss only
    den.SetFields{"last_seen": time.Now()}, // applied on both paths
    []where.Condition{where.Field("email").Eq("x@y.z")},
)
if inserted {
    log.Println("created new user")
}
```

Concurrent inserts that both miss race on the unique constraint — one wins, the other gets `ErrDuplicate`. There is no internal retry; callers decide.

→ [`FindOneAndUpsert`](crud.md#findoneandupsert)

---

## Atomic counter increment

`FindOneAndUpdate` plus a tx for the read-then-write. The simplest correct version under contention uses `ForUpdate` to lock the row:

```go
err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    counter, err := den.NewQuery[Counter](tx,
        where.Field("name").Eq("page_views"),
    ).ForUpdate().First(ctx)
    if err != nil {
        return err
    }
    counter.Value++
    return den.Update(ctx, tx, counter)
})
```

For high-throughput counters that don't need exact consistency, consider sharded counters (one row per shard, sum on read).

→ [`ForUpdate`](queries.md#row-locking) · [`RunInTransaction`](transactions.md)

---

## Claim one job (queue-style worker)

The canonical "worker pool" pattern: each goroutine claims a single pending job, marks it in-flight, and releases the lock. `ForUpdate(SkipLocked())` lets workers race without blocking each other:

```go
err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    job, err := den.NewQuery[Job](tx,
        where.Field("status").Eq("pending"),
    ).Sort("created_at", den.Asc).
      Limit(1).
      ForUpdate(den.SkipLocked()).
      First(ctx)
    if errors.Is(err, den.ErrNotFound) {
        return nil // no work right now
    }
    if err != nil {
        return err
    }
    job.Status = "in_flight"
    job.WorkerID = workerID
    return den.Update(ctx, tx, job)
})
```

`SkipLocked()` skips rows another worker already locked instead of blocking. PostgreSQL maps this to `FOR UPDATE SKIP LOCKED`; SQLite serializes writers via IMMEDIATE tx so the option is a no-op there (one worker at a time anyway).

→ [`ForUpdate`](queries.md#row-locking)

---

## Top-N with grouping

Server-side Top-N — compute groups, sort by an aggregate, limit. No Go-side post-processing:

```go
type Top struct {
    Category string  `den:"group_key"`
    Total    float64 `den:"sum:price"`
}

var top []Top
err := den.NewQuery[Sale](db).
    Limit(5).
    GroupBy("category").
    OrderByAgg(den.OpSum, "price", den.Desc).
    Into(ctx, &top)
```

Sorting by group key uses the parent `Sort(...)`; sorting by an aggregate uses `OrderByAgg(op, field, dir)` because no source-field name identifies the synthetic aggregate column. `Limit` / `Skip` cap and offset the *group rows*, not the underlying documents.

→ [Aggregations](aggregations.md)

---

## Find with eager-loaded links

By default `Link[T].Value` is `nil` — explicit hydration via `WithFetchLinks()` resolves them in one batched IN-query per nesting level:

```go
posts, err := den.NewQuery[Post](db,
    where.Field("status").Eq("published"),
).WithFetchLinks().All(ctx)
// each post.Author.Value is now non-nil
```

Forgetting `WithFetchLinks()` and dereferencing `.Value` is one of the more common new-user bugs — the linter won't catch it. If you need deeper nesting, `WithNestingDepth(n)` overrides the default of 3 levels.

→ [Relations](relations.md)

---

## Cursor pagination

Stable pagination across writes: cursor on `_id` (ULIDs sort chronologically) instead of offset:

```go
const pageSize = 50

// First page
page, err := den.NewQuery[Post](db).Sort("_id", den.Asc).Limit(pageSize).All(ctx)

// Subsequent pages
last := page[len(page)-1].ID
next, err := den.NewQuery[Post](db).After(last).Sort("_id", den.Asc).Limit(pageSize).All(ctx)
```

`After(id)` and `Before(id)` translate to `_id > ?` / `_id < ?`. Sorting by `_id` is required to make the cursor meaningful. Mixing cursor with offset (`Skip`) returns `ErrIncompatiblePagination` — the two pagination styles have no defined interaction.

→ [Queries](queries.md#cursor-pagination)

---

## Upsert by unique field

You have a stream of records to ingest, and the natural deduplication key is a unique column (email, SKU, slug). `FindOneAndUpsert` does the right thing in one round-trip per record:

```go
for _, record := range incoming {
    _, _, err := den.FindOneAndUpsert[Customer](ctx, db,
        &Customer{Email: record.Email, Name: record.Name, Source: "import"},
        den.SetFields{"name": record.Name, "last_synced_at": time.Now()},
        []where.Condition{where.Field("email").Eq(record.Email)},
    )
    if err != nil {
        return err
    }
}
```

If two ingest workers race on the same email, one wins and the other gets `ErrDuplicate` from the unique constraint — handle by retrying, logging, or surfacing depending on your semantics.

For *batch* ingests with no per-record uniqueness needs, prefer `InsertMany(ctx, db, docs, den.ContinueOnError())` — much faster, returns an `*InsertManyError` listing per-document failures by input index.

→ [`FindOneAndUpsert`](crud.md#findoneandupsert) · [`InsertMany`](crud.md#insertmany)
