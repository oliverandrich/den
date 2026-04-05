package dentest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oliverandrich/den"
	_ "github.com/oliverandrich/den/backend/postgres" // register postgres:// scheme
	_ "github.com/oliverandrich/den/backend/sqlite"   // register sqlite:// scheme
)

// PostgresURL returns the PostgreSQL connection string from the
// DEN_POSTGRES_URL environment variable, falling back to a local default.
func PostgresURL() string {
	url := os.Getenv("DEN_POSTGRES_URL")
	if url == "" {
		url = "postgres://localhost/den_test"
	}
	return url
}

// MustOpen creates a file-backed SQLite Den database in a temp directory for testing.
// It registers the given document types and automatically closes
// the database when the test ends.
func MustOpen(t testing.TB, types ...any) *den.DB {
	return MustOpenWith(t, types, nil)
}

// MustOpenPostgres creates a PostgreSQL-backed Den database for testing.
func MustOpenPostgres(t testing.TB, connString string, types ...any) *den.DB {
	return MustOpenPostgresWith(t, connString, types, nil)
}

// MustOpenWith creates a file-backed SQLite Den database with options.
func MustOpenWith(t testing.TB, types []any, opts []den.Option) *den.DB {
	t.Helper()

	dsn := "sqlite:///" + filepath.Join(t.TempDir(), "test.db")
	db, err := den.OpenURL(dsn, opts...)
	if err != nil {
		t.Fatalf("dentest: open sqlite: %v", err)
	}

	if len(types) > 0 {
		if err := den.Register(context.Background(), db, types...); err != nil {
			_ = db.Close()
			t.Fatalf("dentest: register types: %v", err)
		}
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

// MustOpenPostgresWith creates a PostgreSQL-backed Den database with options.
// Registered collections are automatically dropped when the test ends.
func MustOpenPostgresWith(t testing.TB, connString string, types []any, opts []den.Option) *den.DB {
	t.Helper()

	db, err := den.OpenURL(connString, opts...)
	if err != nil {
		t.Fatalf("dentest: open postgres: %v", err)
	}

	if len(types) > 0 {
		if err := den.Register(context.Background(), db, types...); err != nil {
			_ = db.Close()
			t.Fatalf("dentest: register types: %v", err)
		}
	}

	t.Cleanup(func() {
		ctx := context.Background()
		for _, name := range den.Collections(db) {
			_ = db.Backend().DropCollection(ctx, name)
		}
		_ = db.Close()
	})
	return db
}
