# Lifecycle Hooks

Documents can implement hook interfaces to run logic before or after database operations. Hooks are called within the same transaction as the operation itself.

## Hook Interfaces

Implement any combination of these interfaces on your document struct:

```go
type BeforeInserter interface {
    BeforeInsert(ctx context.Context) error
}

type AfterInserter interface {
    AfterInsert(ctx context.Context) error
}

type BeforeUpdater interface {
    BeforeUpdate(ctx context.Context) error
}

type AfterUpdater interface {
    AfterUpdate(ctx context.Context) error
}

type BeforeDeleter interface {
    BeforeDelete(ctx context.Context) error
}

type AfterDeleter interface {
    AfterDelete(ctx context.Context) error
}

type BeforeSoftDeleter interface {
    BeforeSoftDelete(ctx context.Context) error
}

type AfterSoftDeleter interface {
    AfterSoftDelete(ctx context.Context) error
}

type BeforeSaver interface {
    BeforeSave(ctx context.Context) error
}

type AfterSaver interface {
    AfterSave(ctx context.Context) error
}

type Validator interface {
    Validate(ctx context.Context) error
}
```

!!! note
    `BeforeSaver` and `AfterSaver` are called by both `Insert` and `Update`. Use them for logic that applies to every write, regardless of whether the document is new or existing.

## Execution Order

Mutating hooks always run **before** validation so that a hook can populate default values, compute derived fields, or normalize inputs before any constraint check. Validation then sees the final document that will actually be persisted.

### Insert

```
BeforeInsert() -> BeforeSave() -> <tag validation> -> Validate() -> [write to DB] -> AfterInsert() -> AfterSave()
```

### Update

```
BeforeUpdate() -> BeforeSave() -> <tag validation> -> Validate() -> [write to DB] -> AfterUpdate() -> AfterSave()
```

### Delete

```
BeforeDelete() -> [delete from DB] -> AfterDelete()
```

### Soft-Delete

When a `SoftDelete`-embedding document is deleted without `HardDelete()`, the soft-only hook pair nests inside the general Delete pair:

```
BeforeDelete() -> BeforeSoftDelete() -> [write soft-delete] -> AfterSoftDelete() -> AfterDelete()
```

`BeforeSoftDelete` and `AfterSoftDelete` do not fire on `HardDelete()` — use them for audit-log side effects that should only run when the document remains in storage.

If any `Before*` hook, tag validation, or `Validate()` returns an error, the operation is aborted and the transaction is rolled back. The error is returned to the caller.

!!! note
    Tag validation (configured via `validate.WithValidation()`) and the custom `Validator.Validate()` method both run after the mutating hooks. This is the same pattern used by ActiveRecord, Django ORM and SQLAlchemy: hooks can set defaults, compute slugs or timestamps, normalize emails, etc., and then validation checks the final state.

## Example: Article with Hooks

```go
type Article struct {
    document.Base
    Title     string `json:"title"      den:"index"`
    Slug      string `json:"slug"       den:"unique"`
    Body      string `json:"body"       den:"fts"`
    WordCount int    `json:"word_count"`
}

// BeforeSave runs on both Insert and Update.
// Derive the slug and word count from the current field values.
func (a *Article) BeforeSave(ctx context.Context) error {
    a.Slug = slugify(a.Title)
    a.WordCount = len(strings.Fields(a.Body))
    return nil
}

// Validate ensures required fields are present before any write.
func (a *Article) Validate(ctx context.Context) error {
    if a.Title == "" {
        return errors.New("title is required")
    }
    if a.Body == "" {
        return errors.New("body is required")
    }
    return nil
}
```

Usage:

```go
article := &Article{
    Title: "Getting Started with Den",
    Body:  "Den is an ODM for Go...",
}

err := den.Insert(ctx, db, article)
// 1. BeforeInsert() -- not implemented, skipped
// 2. BeforeSave() -- sets slug="getting-started-with-den", word_count=6
// 3. Validate() -- checks title and body are non-empty (sees the final document)
// 4. Write to database
// 5. AfterInsert() -- not implemented, skipped
// 6. AfterSave() -- not implemented, skipped
```

### Defaulting before validation

Because mutating hooks run before validation, you can use `BeforeInsert` to populate a default value for a field that validation requires:

```go
type Page struct {
    document.Base
    Title string `json:"title"`
    Slug  string `json:"slug" validate:"required"`
}

func (p *Page) BeforeInsert(ctx context.Context) error {
    if p.Slug == "" {
        p.Slug = slugify(p.Title)
    }
    return nil
}
```

A `Page` can be inserted with only `Title` set — the hook populates `Slug`, then tag validation passes because `Slug` is now non-empty.

## Aborting an Operation

Return an error from any `Before*` hook to prevent the write:

```go
func (a *Article) BeforeDelete(ctx context.Context) error {
    if a.Protected {
        return errors.New("cannot delete a protected article")
    }
    return nil
}
```

```go
err := den.Delete(ctx, db, article)
// err: "cannot delete a protected article"
// The document remains in the database.
```

## Hooks and Transactions

All hooks run within the same database transaction as the operation. This means:

- A `BeforeSaver` that modifies fields is part of the atomic write.
- An `AfterInserter` that fails causes the entire insert (including the document write) to roll back.
- Inside `RunInTransaction`, hooks execute within the outer transaction.

```go
err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
    // Both inserts (and their hooks) share the same transaction.
    // If the second insert's Validate() fails, both are rolled back.
    if err := den.Insert(ctx, tx, article1); err != nil {
        return err
    }
    return den.Insert(ctx, tx, article2)
})
```

!!! warning
    Avoid performing long-running or external operations (HTTP calls, file I/O) inside hooks. Hooks hold the database transaction open for their entire duration. On SQLite, this blocks all other writers.
