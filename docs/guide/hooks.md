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

type BeforeSaver interface {
    BeforeSave(ctx context.Context) error
}

type AfterSaver interface {
    AfterSave(ctx context.Context) error
}

type Validator interface {
    Validate() error
}
```

!!! note
    `BeforeSaver` and `AfterSaver` are called by both `Insert` and `Update`. Use them for logic that applies to every write, regardless of whether the document is new or existing.

## Execution Order

### Insert

```
Validate() -> BeforeInsert() -> BeforeSave() -> [write to DB] -> AfterInsert() -> AfterSave()
```

### Update

```
Validate() -> BeforeUpdate() -> BeforeSave() -> [write to DB] -> AfterUpdate() -> AfterSave()
```

### Delete

```
BeforeDelete() -> [delete from DB] -> AfterDelete()
```

If any `Before*` hook or `Validate()` returns an error, the operation is aborted and the transaction is rolled back. The error is returned to the caller.

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
func (a *Article) Validate() error {
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
// 1. Validate() -- checks title and body are non-empty
// 2. BeforeInsert() -- not implemented, skipped
// 3. BeforeSave() -- sets slug="getting-started-with-den", word_count=6
// 4. Write to database
// 5. AfterInsert() -- not implemented, skipped
// 6. AfterSave() -- not implemented, skipped
```

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
    if err := den.TxInsert(tx, article1); err != nil {
        return err
    }
    return den.TxInsert(tx, article2)
})
```

!!! warning
    Avoid performing long-running or external operations (HTTP calls, file I/O) inside hooks. Hooks hold the database transaction open for their entire duration. On SQLite, this blocks all other writers.
