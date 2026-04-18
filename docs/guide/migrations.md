# Migrations

## When to Use Migrations

Documents in Den are schema-flexible: adding new fields with zero values works without any migration. Explicit migrations are needed for:

- **Field renames** (e.g., `name` to `title`)
- **Type changes** (e.g., `string` to `int`)
- **Data transformations** (e.g., splitting a full name into first/last)

## Running Migrations at Startup

Migrations do not run automatically. You are responsible for calling `r.Up()` at the right point in your application lifecycle — typically after opening the database and registering document types:

```go
func main() {
    ctx := context.Background()

    db, err := den.OpenURL(ctx, "sqlite:///data.db")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Register document types first
    if err := den.Register(ctx, db, &Note{}, &User{}); err != nil {
        log.Fatal(err)
    }

    // Run pending migrations
    r := setupMigrations()
    if err := r.Up(ctx, db); err != nil {
        log.Fatal(err)
    }

    // Application is ready
}
```

!!! note
    `r.Up()` is idempotent — it skips migrations that have already been applied. It is safe to call on every startup.

!!! tip "Using Burrow?"
    If you are using Den through [Burrow](https://burrow.readthedocs.io/), migrations are handled automatically by the framework. See the [Burrow Migrations Guide](https://burrow.readthedocs.io/en/stable/guide/migrations/) for details.

## Defining Migrations

Migrations are Go functions registered with a version identifier:

```go
import "github.com/oliverandrich/den/migrate"

func setupMigrations() *migrate.Registry {
    r := migrate.NewRegistry()

    r.Register("20250402_001_rename_name_to_title", migrate.Migration{
        Forward: func(ctx context.Context, tx *den.Tx) error {
            for note, err := range den.NewQuery[OldNote](db).Iter(ctx) {
                if err != nil {
                    return err
                }
                note.Title = note.Name
                if err := den.TxUpdate(tx, note); err != nil {
                    return err
                }
            }
            return nil
        },
        Backward: func(ctx context.Context, tx *den.Tx) error {
            for note, err := range den.NewQuery[Note](db).Iter(ctx) {
                if err != nil {
                    return err
                }
                note.Name = note.Title
                if err := den.TxUpdate(tx, note); err != nil {
                    return err
                }
            }
            return nil
        },
    })

    return r
}
```

!!! tip
    Use a timestamp-based naming convention like `YYYYMMDD_NNN_description` for migration versions. This ensures consistent ordering across environments.

## Running Migrations

```go
r := setupMigrations()

// Run all pending forward migrations
err := r.Up(ctx, db)

// Run one forward migration
err := r.UpOne(ctx, db)

// Roll back one migration
err := r.DownOne(ctx, db)

// Roll back all migrations
err := r.Down(ctx, db)
```

## Migration Tracking

Den stores migration state in a `_den_migrations` table. Each applied migration is recorded with its version string and timestamp. `r.Up()` compares registered migrations against this log to determine which are pending.

## Transaction Safety

Each migration runs within a Den transaction:

- If the migration function returns `nil`, the transaction is committed
- If it returns an error, the transaction is rolled back and the migration is marked as failed
- Subsequent migrations are not executed after a failure

!!! warning
    If a migration fails, fix the issue and re-run `r.Up()`. Den will retry only the failed and pending migrations.

## Streaming with Iter()

For migrations that touch many documents, use `Iter()` to stream documents without loading them all into memory:

```go
r.Register("20250410_001_normalize_prices", migrate.Migration{
    Forward: func(ctx context.Context, tx *den.Tx) error {
        for product, err := range den.NewQuery[Product](db).Iter(ctx) {
            if err != nil {
                return err
            }
            product.Price = math.Round(product.Price*100) / 100
            if err := den.TxUpdate(tx, product); err != nil {
                return err
            }
        }
        return nil
    },
})
```

## Error Handling

Migration errors are wrapped with `den.ErrMigrationFailed`:

```go
err := r.Up(ctx, db)
if errors.Is(err, den.ErrMigrationFailed) {
    log.Fatal("Migration failed:", err)
}
```
