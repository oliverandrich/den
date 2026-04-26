# 10-Minute Tour

A linear walkthrough that gets you from `go get` to a working app exercising every major Den feature once: types, registration, CRUD, queries, soft-delete, relations, hooks, and indexes. Read it top-to-bottom; each section assumes the previous ones.

The example builds a tiny task tracker — `Project` documents that link to `Task` documents, with hooks, validation, soft delete, and an indexed status field.

---

## 1. Project setup

```bash
mkdir tracker && cd tracker
go mod init tracker
go get github.com/oliverandrich/den@latest
```

Drop the following into `main.go`. The whole file compiles and runs end-to-end; later sections add code on top of what's already there.

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"

    "github.com/oliverandrich/den"
    _ "github.com/oliverandrich/den/backend/sqlite" // register sqlite:// scheme
    "github.com/oliverandrich/den/document"
    "github.com/oliverandrich/den/where"
)

func main() {
    ctx := context.Background()
    _ = ctx
}
```

Run it (`go run .`) to confirm the toolchain and dependency wiring work. We'll fill in `main` step by step.

---

## 2. Define and register the document types

A document is a Go struct that embeds `document.Base`. The `den` tag carries metadata (indexes, uniqueness, full-text); the `json` tag carries the field name. Composable embeds add features: `document.SoftDelete` opts the type into soft delete.

```go
type Project struct {
    document.Base
    Name string `json:"name" den:"unique"` // unique across all Projects
}

type Task struct {
    document.Base
    document.SoftDelete                                       // adds DeletedAt + filtered queries
    ProjectLink den.Link[Project] `json:"project"`            // typed reference
    Title       string            `json:"title"`
    Status      string            `json:"status" den:"index"` // indexed for status filters
}
```

Open the database and register both types in one expression:

```go
db, err := den.OpenURL(ctx, "sqlite:///tracker.db",
    den.WithTypes(&Project{}, &Task{}),
)
if err != nil { log.Fatal(err) }
defer db.Close()
```

Registration creates the `project` and `task` tables, the unique index on `Project.Name`, the secondary index on `Task.Status`, and the soft-delete machinery on Task. It's idempotent — calling on every startup is safe.

---

## 3. Add a hook

Any document can implement hook interfaces to run code at lifecycle points. They're plain Go methods on the type — no registration, no decorators.

```go
func (t *Task) BeforeInsert(ctx context.Context) error {
    if t.Status == "" {
        t.Status = "todo" // default if the caller didn't set it
    }
    return nil
}

func (t *Task) Validate(ctx context.Context) error {
    if t.Title == "" {
        return errors.New("task title is required")
    }
    return nil
}
```

`BeforeInsert` runs before validation, so a missing title still fails (`Validate` runs on the post-hook state). Den has a hook for every CRUD verb plus `Validator`, `BeforeSaver`, `AfterSaver`, and soft-delete pairs — see [Lifecycle Hooks](../guide/hooks.md) for the full list and ordering.

---

## 4. Insert and link

Insert a Project, then a Task that links to it. `den.NewLink(parent)` extracts the parent's ID into the Link.

```go
proj := &Project{Name: "Den Docs"}
if err := den.Insert(ctx, db, proj); err != nil { log.Fatal(err) }

task := &Task{
    ProjectLink: den.NewLink(proj),
    Title:       "Write 10-minute tour",
}
if err := den.Insert(ctx, db, task); err != nil { log.Fatal(err) }

fmt.Println("created", proj.ID, task.ID, "status:", task.Status)
```

The `BeforeInsert` hook populated `task.Status` to `"todo"`; `Validate` accepted the non-empty title.

`Link[T]` stores only the ID in JSON. To dereference, you either fetch the linked doc separately or hydrate via `WithFetchLinks()` (next section).

---

## 5. Query with conditions and eager links

Find all open tasks for a project, with the linked Project hydrated:

```go
tasks, err := den.NewQuery[Task](db,
    where.Field("project").Eq(proj.ID),
    where.Field("status").Eq("todo"),
).WithFetchLinks().Sort("_created_at", den.Desc).All(ctx)
if err != nil { log.Fatal(err) }

for _, t := range tasks {
    fmt.Printf("- [%s] %s (project: %s)\n", t.Status, t.Title, t.ProjectLink.Value.Name)
}
```

Without `WithFetchLinks()`, `task.ProjectLink.Value` is `nil` (only `task.ProjectLink.ID` is populated). The `den:"index"` tag on `Status` makes the second condition use a real index instead of a JSONB scan.

For streaming over large result sets, swap `.All(ctx)` for [`.Iter(ctx)`](../guide/queries.md#iteration) — Den returns a Go 1.23 `iter.Seq2[*T, error]` that releases memory as you go.

---

## 6. Update one field atomically

The single-field update pattern: route through `FindOneAndUpdate` so you avoid the read-modify-write round-trip and any concurrent-update race.

```go
done, err := den.FindOneAndUpdate[Task](ctx, db,
    den.SetFields{"status": "done"},
    []where.Condition{where.Field("_id").Eq(task.ID)},
)
if err != nil { log.Fatal(err) }
fmt.Println("marked done:", done.Title, "→", done.Status)
```

`SetFields` keys are JSON tag names (`"status"`, not `"Status"`). Mistakes are caught before the write opens — see the [Recipes](../guide/recipes.md) page for more single-purpose patterns.

---

## 7. Soft-delete and querying deleted rows

Because Task embeds `document.SoftDelete`, `Delete` flips `DeletedAt` instead of removing the row, and queries auto-filter the deleted row out. To see deleted rows, opt in via `IncludeDeleted()`:

```go
if err := den.Delete(ctx, db, done); err != nil { log.Fatal(err) }

active, _ := den.NewQuery[Task](db).Count(ctx)
all, _    := den.NewQuery[Task](db).IncludeDeleted().Count(ctx)
fmt.Printf("active: %d, including deleted: %d\n", active, all)
```

If you actually want the row gone, pass `den.HardDelete()` to `Delete`. To clean up cascades (deleted parent → linked children), pass `den.WithLinkRule(den.LinkDelete)`. See [Soft Delete](../guide/soft-delete.md) and [Relations](../guide/relations.md).

---

## 8. Inspect indexes

Den's secondary indexes are real SQL indexes. `Meta` returns the runtime metadata for any registered type:

```go
meta, _ := den.Meta[Task](db)
for _, idx := range meta.Indexes {
    fmt.Printf("- %s: %v (unique=%v)\n", idx.Name, idx.Fields, idx.Unique)
}
```

You'll see the index Den auto-created for `Status` plus the soft-delete filter index. Add more by tagging more fields, or use `unique_together`/`index_together` for composite indexes. Drift between code and DB can be caught with the [stale-index sweep](../guide/migrations.md#dropping-stale-indexes).

---

## You shipped a working app

That's the full surface in ten minutes:

- **Types and registration**: §2
- **Lifecycle hooks and validation**: §3
- **Insert + typed relations**: §4
- **Queries with eager links + indexes**: §5
- **Atomic field updates**: §6
- **Soft delete**: §7
- **Index inspection**: §8

From here:

- [CRUD Operations](../guide/crud.md) for the rest of the write surface (`Update`, `InsertMany`, `FindOneAndUpsert`, …)
- [Queries](../guide/queries.md) for cursor pagination, projections, aggregations, FTS
- [Relations](../guide/relations.md) for cascade rules, BackLinks, nested fetch
- [Hooks](../guide/hooks.md) for the full hook ordering across paths
- [Recipes](../guide/recipes.md) for copy-paste patterns
- [Reference / Struct Tags](../reference/struct-tags.md) for every `den:` value with its context
