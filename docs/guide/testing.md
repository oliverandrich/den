# Testing

## SQLite Test Helper

The `dentest` package provides a one-liner to create a file-backed SQLite database in a temporary directory, pre-register document types, and auto-close when the test ends:

```go
import (
    "context"
    "testing"

    "github.com/oliverandrich/den"
    "github.com/oliverandrich/den/dentest"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestProductInsert(t *testing.T) {
    db := dentest.MustOpen(t, &Product{}, &Category{})

    ctx := context.Background()
    p := &Product{Name: "Widget", Price: 9.99}
    err := den.Insert(ctx, db, p)
    require.NoError(t, err)
    assert.NotEmpty(t, p.ID)

    found, err := den.FindByID[Product](ctx, db, p.ID)
    require.NoError(t, err)
    assert.Equal(t, "Widget", found.Name)
    assert.Equal(t, 9.99, found.Price)
}
```

`dentest.MustOpen` creates a real SQLite database file inside `t.TempDir()` and registers `t.Cleanup` to close it automatically. No manual teardown needed.

## PostgreSQL Test Helper

For testing against PostgreSQL, use `dentest.MustOpenPostgres`:

```go
func TestProductInsertPG(t *testing.T) {
    connStr := "postgres://user:pass@localhost/testdb"
    db := dentest.MustOpenPostgres(t, connStr, &Product{}, &Category{})

    ctx := context.Background()
    p := &Product{Name: "Widget", Price: 9.99}
    err := den.Insert(ctx, db, p)
    require.NoError(t, err)

    found, err := den.FindByID[Product](ctx, db, p.ID)
    require.NoError(t, err)
    assert.Equal(t, "Widget", found.Name)
}
```

!!! note "Cleanup behavior"
    `MustOpenPostgres` does not create a fresh schema — it connects to the database you supply. On `t.Cleanup`, it drops every collection that was registered through the helper (by name) and then closes the connection. Data in unrelated collections is untouched.

!!! tip "Picking a connection string"
    Use `dentest.PostgresURL()` to pull the DSN from the `DEN_POSTGRES_URL` environment variable (default `postgres://localhost/den_test`). This keeps tests portable between developer machines and CI.

!!! warning "Parallel tests and collection names"
    Tests that run in parallel against the same database must register disjoint document types, or each test must use its own database. Two tests registering `Product` against the same database will race on the shared collection; the helper does not sandbox them.

## Complete Test Example

A test that inserts, queries, updates, and deletes:

```go
func TestProductLifecycle(t *testing.T) {
    db := dentest.MustOpen(t, &Product{})
    ctx := context.Background()

    // Insert
    p := &Product{Name: "Gadget", Price: 19.99}
    require.NoError(t, den.Insert(ctx, db, p))
    assert.NotEmpty(t, p.ID)

    // Query
    products, err := den.NewQuery[Product](db,
        where.Field("price").Gt(10.0),
    ).All(ctx)
    require.NoError(t, err)
    assert.Len(t, products, 1)
    assert.Equal(t, "Gadget", products[0].Name)

    // Update
    p.Price = 24.99
    require.NoError(t, den.Update(ctx, db, p))

    refreshed, err := den.FindByID[Product](ctx, db, p.ID)
    require.NoError(t, err)
    assert.Equal(t, 24.99, refreshed.Price)

    // Delete
    require.NoError(t, den.Delete(ctx, db, p))

    _, err = den.FindByID[Product](ctx, db, p.ID)
    assert.ErrorIs(t, err, den.ErrNotFound)
}
```

!!! tip
    Use [testify](https://github.com/stretchr/testify) for assertions. `require` aborts the test on failure (use for setup steps), `assert` records the failure and continues (use for verification steps).
