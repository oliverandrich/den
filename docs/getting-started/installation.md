# Installation

## Prerequisites

- **Go 1.25** or later

## Install Den

```bash
go get github.com/oliverandrich/den@latest
```

## Backend Imports

Den requires at least one backend. Backends register themselves via side-effect imports — you import the package with a blank identifier (`_`) and the backend becomes available to `den.OpenURL`.

=== "SQLite"

    ```go
    import _ "github.com/oliverandrich/den/backend/sqlite"
    ```

    The SQLite backend is pure Go (no CGO required). It compiles into your binary with zero external dependencies.

=== "PostgreSQL"

    ```go
    import _ "github.com/oliverandrich/den/backend/postgres"
    ```

    The PostgreSQL backend uses [pgx](https://github.com/jackc/pgx) and requires a running PostgreSQL instance.

=== "Both"

    ```go
    import (
        _ "github.com/oliverandrich/den/backend/sqlite"
        _ "github.com/oliverandrich/den/backend/postgres"
    )
    ```

    Import both when your application needs to support either backend at runtime.

!!! note "How side-effect imports work"
    Each backend package has an `init()` function that registers its URL scheme (`sqlite://` or `postgres://`) with Den's backend registry. When you call `den.OpenURL(dsn)`, Den matches the scheme to the registered backend and opens the connection.

!!! warning "PostgreSQL requires a running instance"
    Unlike SQLite, the PostgreSQL backend connects to an external database server. Make sure PostgreSQL is running and accessible before opening a connection.

## Verify Installation

```bash
go build ./...
```

If the build succeeds, Den is installed and ready to use.
