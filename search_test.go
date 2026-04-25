package den_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/where"
)

type FTSArticle struct {
	document.Base
	Title    string `json:"title" den:"fts"`
	Body     string `json:"body" den:"fts"`
	Category string `json:"category"`
}

func TestSearch_SQLite(t *testing.T) {
	db := dentest.MustOpen(t, &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*FTSArticle{
		{Title: "Go Programming", Body: "Learn Go language basics", Category: "tech"},
		{Title: "Python Tutorial", Body: "Introduction to Python", Category: "tech"},
		{Title: "Cooking Tips", Body: "How to make pasta", Category: "food"},
	}))

	results, err := den.NewQuery[FTSArticle](db).Search(ctx, "Go")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Go Programming", results[0].Title)
}

// TestSearch_HonorsScopeInTx pins that Search routes through the caller's
// scope just like every other operation: a doc inserted inside the caller's
// tx is visible to a tx-bound Search before commit. SQLite FTS5 triggers
// fire on the same connection; PostgreSQL's tsvector + GIN see same-tx
// writes via MVCC. Both backends share the contract.
func TestSearch_HonorsScopeInTx(t *testing.T) {
	dbs := map[string]*den.DB{
		"sqlite":   dentest.MustOpen(t, &FTSArticle{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{}),
	}
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			var insideHits []*FTSArticle
			err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
				if err := den.Insert(ctx, tx, &FTSArticle{
					Title: "TxLocalSecret", Body: "tx-local body", Category: "tech",
				}); err != nil {
					return err
				}
				hits, err := den.NewQuery[FTSArticle](tx).Search(ctx, "TxLocalSecret")
				if err != nil {
					return err
				}
				insideHits = hits
				return nil
			})
			require.NoError(t, err)
			require.Len(t, insideHits, 1, "tx-bound Search must see tx-local writes")
			assert.Equal(t, "TxLocalSecret", insideHits[0].Title)

			after, err := den.NewQuery[FTSArticle](db).Search(ctx, "TxLocalSecret")
			require.NoError(t, err)
			require.Len(t, after, 1, "doc remains visible after commit")
		})
	}
}

// TestSearch_RollbackHidesDocs pins the isolation guarantee: if the tx
// rolls back, the in-tx-Searchable doc never reaches committed state and
// is invisible to a fresh DB-bound Search.
func TestSearch_RollbackHidesDocs(t *testing.T) {
	dbs := map[string]*den.DB{
		"sqlite":   dentest.MustOpen(t, &FTSArticle{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{}),
	}
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			rollbackErr := errors.New("force rollback")
			err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
				if err := den.Insert(ctx, tx, &FTSArticle{
					Title: "ShouldNeverReachDisk", Body: "rolled-back body", Category: "tech",
				}); err != nil {
					return err
				}
				// Confirm the doc IS visible in-tx before we roll back —
				// otherwise the rollback test would pass even if Search were
				// still bypassing the tx (silent regression).
				hits, err := den.NewQuery[FTSArticle](tx).Search(ctx, "ShouldNeverReachDisk")
				if err != nil {
					return err
				}
				require.Len(t, hits, 1, "doc must be visible in tx pre-rollback")
				return rollbackErr
			})
			require.ErrorIs(t, err, rollbackErr)

			after, err := den.NewQuery[FTSArticle](db).Search(ctx, "ShouldNeverReachDisk")
			require.NoError(t, err)
			assert.Empty(t, after, "rolled-back doc must not be Searchable from db scope")
		})
	}
}

// TestSearch_TxBoundWhereSeesTxLocal verifies that Where conditions applied
// alongside Search on a tx-bound QuerySet also operate on tx-local data —
// the whole query travels through the tx connection, not just the FTS bit.
func TestSearch_TxBoundWhereSeesTxLocal(t *testing.T) {
	dbs := map[string]*den.DB{
		"sqlite":   dentest.MustOpen(t, &FTSArticle{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{}),
	}
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			var insideHits []*FTSArticle
			err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
				if err := den.InsertMany(ctx, tx, []*FTSArticle{
					{Title: "TechPost", Body: "shared keyword tech", Category: "tech"},
					{Title: "FoodPost", Body: "shared keyword food", Category: "food"},
				}); err != nil {
					return err
				}
				hits, err := den.NewQuery[FTSArticle](tx,
					where.Field("category").Eq("tech"),
				).Search(ctx, "shared")
				if err != nil {
					return err
				}
				insideHits = hits
				return nil
			})
			require.NoError(t, err)
			require.Len(t, insideHits, 1, "Where filter must apply against tx-local data")
			assert.Equal(t, "TechPost", insideHits[0].Title)
		})
	}
}

func TestSearch_SQLite_MultipleResults(t *testing.T) {
	db := dentest.MustOpen(t, &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*FTSArticle{
		{Title: "Go Basics", Body: "Learn Go", Category: "tech"},
		{Title: "Advanced Go", Body: "Go concurrency patterns", Category: "tech"},
		{Title: "Python Basics", Body: "Learn Python", Category: "tech"},
	}))

	results, err := den.NewQuery[FTSArticle](db).Search(ctx, "Go")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestSearch_SQLite_WithFilter(t *testing.T) {
	db := dentest.MustOpen(t, &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*FTSArticle{
		{Title: "Go Web", Body: "Building web apps with Go", Category: "tech"},
		{Title: "Go Cooking", Body: "Go to recipes for beginners", Category: "food"},
	}))

	results, err := den.NewQuery[FTSArticle](db, where.Field("category").Eq("tech")).Search(ctx, "Go")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Go Web", results[0].Title)
}

func TestSearch_SQLite_WithLimit(t *testing.T) {
	db := dentest.MustOpen(t, &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*FTSArticle{
		{Title: "Go One", Body: "First Go article", Category: "tech"},
		{Title: "Go Two", Body: "Second Go article", Category: "tech"},
		{Title: "Go Three", Body: "Third Go article", Category: "tech"},
	}))

	results, err := den.NewQuery[FTSArticle](db).Limit(2).Search(ctx, "Go")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestSearch_SQLite_NoResults(t *testing.T) {
	db := dentest.MustOpen(t, &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, den.Insert(ctx, db, &FTSArticle{Title: "Hello", Body: "World"}))

	results, err := den.NewQuery[FTSArticle](db).Search(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestSearch_Postgres(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*FTSArticle{
		{Title: "Go Programming", Body: "Learn Go language basics", Category: "tech"},
		{Title: "Python Tutorial", Body: "Introduction to Python", Category: "tech"},
		{Title: "Cooking Tips", Body: "How to make pasta", Category: "food"},
	}))

	results, err := den.NewQuery[FTSArticle](db).Search(ctx, "programming")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Go Programming", results[0].Title)
}

func TestSearch_Postgres_WithFilter(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*FTSArticle{
		{Title: "Go Web", Body: "Building web apps with Go", Category: "tech"},
		{Title: "Go Cooking", Body: "Go to recipes for beginners", Category: "food"},
	}))

	results, err := den.NewQuery[FTSArticle](db, where.Field("category").Eq("tech")).Search(ctx, "Go")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Go Web", results[0].Title)
}

func TestSearch_Postgres_WithLimit(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*FTSArticle{
		{Title: "Go One", Body: "First Go article", Category: "tech"},
		{Title: "Go Two", Body: "Second Go article", Category: "tech"},
		{Title: "Go Three", Body: "Third Go article", Category: "tech"},
	}))

	results, err := den.NewQuery[FTSArticle](db).Limit(2).Search(ctx, "Go")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

// TestSearch_HonorsAfterCursor pins that Search respects After(id) on
// both backends, matching the non-FTS QuerySet behavior. The rank-based
// default order is overridden with an explicit Sort("_id") so the cursor
// semantics are predictable across backends.
func TestSearch_HonorsAfterCursor(t *testing.T) {
	dbs := map[string]*den.DB{
		"sqlite":   dentest.MustOpen(t, &FTSArticle{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{}),
	}
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, den.InsertMany(ctx, db, []*FTSArticle{
				{Title: "Go One", Body: "First Go article"},
				{Title: "Go Two", Body: "Second Go article"},
				{Title: "Go Three", Body: "Third Go article"},
			}))

			first, err := den.NewQuery[FTSArticle](db).Sort("_id", den.Asc).Search(ctx, "Go")
			require.NoError(t, err)
			require.Len(t, first, 3)

			rest, err := den.NewQuery[FTSArticle](db).Sort("_id", den.Asc).After(first[0].ID).Search(ctx, "Go")
			require.NoError(t, err)
			require.Len(t, rest, 2, "After(id) must be honored in FTS Search")
			assert.Equal(t, first[1].ID, rest[0].ID)
			assert.Equal(t, first[2].ID, rest[1].ID)
		})
	}
}

func TestSearch_Postgres_NoResults(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, den.Insert(ctx, db, &FTSArticle{Title: "Hello", Body: "World"}))

	results, err := den.NewQuery[FTSArticle](db).Search(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, results)
}
