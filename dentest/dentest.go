package dentest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/oliverandrich/den"
	pgbackend "github.com/oliverandrich/den/backend/postgres"
	sqlitebackend "github.com/oliverandrich/den/backend/sqlite"
)

// MustOpen creates a file-backed SQLite Den database in a temp directory for testing.
// It registers the given document types and automatically closes
// the database when the test ends.
func MustOpen(t *testing.T, types ...any) *den.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	backend, err := sqlitebackend.Open(dbPath)
	if err != nil {
		t.Fatalf("dentest: open sqlite: %v", err)
	}

	db, err := den.Open(backend)
	if err != nil {
		_ = backend.Close()
		t.Fatalf("dentest: open db: %v", err)
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

// MustOpenPostgres creates a PostgreSQL-backed Den database for testing.
func MustOpenPostgres(t *testing.T, connString string, types ...any) *den.DB {
	t.Helper()

	backend, err := pgbackend.Open(connString)
	if err != nil {
		t.Fatalf("dentest: open postgres: %v", err)
	}

	db, err := den.Open(backend)
	if err != nil {
		_ = backend.Close()
		t.Fatalf("dentest: open db: %v", err)
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
