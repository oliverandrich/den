//go:build postgres

package bench

import (
	"context"
	"os"
	"testing"

	"github.com/oliverandrich/den"
	pgbackend "github.com/oliverandrich/den/backend/postgres"
)

func init() {
	backends = append(backends, backendFactory{"postgres", setupPostgres})
	concurrentBackends = append(concurrentBackends, backendFactory{"postgres", setupPostgres})
	concurrentWriteBackends = append(concurrentWriteBackends, backendFactory{"postgres", setupPostgres})
}

func setupPostgres(b *testing.B, types ...any) *den.DB {
	b.Helper()
	ctx := context.Background()
	url := os.Getenv("DEN_POSTGRES_URL")
	if url == "" {
		b.Skip("DEN_POSTGRES_URL not set")
	}

	backend, err := pgbackend.Open(url)
	if err != nil {
		b.Fatal(err)
	}
	db, err := den.Open(backend)
	if err != nil {
		b.Fatal(err)
	}

	// Clean up any previous test tables
	backend.DropCollection(ctx, "user")
	backend.DropCollection(ctx, "article")
	backend.DropCollection(ctx, "comment")

	if len(types) == 0 {
		types = []any{&User{}, &Article{}, &Comment{}}
	}
	if err := den.Register(ctx, db, types...); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		backend.DropCollection(ctx, "user")
		backend.DropCollection(ctx, "article")
		backend.DropCollection(ctx, "comment")
		db.Close()
	})
	return db
}
