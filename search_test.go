package den_test

import (
	"context"
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

// TestSearch_TxLocalDocInvisibleUntilCommit pins the documented Tx-
// visibility caveat on both backends: FTS reads go to the DB backend,
// not the tx, so a doc inserted inside the caller's tx is not Searchable
// until the tx commits. The Postgres variant rules out any MVCC-based
// same-tx visibility — both backends behave identically because the
// caveat is about Search's routing decision, not about index timing.
func TestSearch_TxLocalDocInvisibleUntilCommit(t *testing.T) {
	dbs := map[string]*den.DB{
		"sqlite":   dentest.MustOpen(t, &FTSArticle{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{}),
	}
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			// Capture the in-tx Search result and assert outside the closure
			// so a failed assertion doesn't leak through with the tx silently
			// committed (assert in-closure would record fail + return nil).
			var insideHits []*FTSArticle
			err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
				if err := den.Insert(ctx, tx, &FTSArticle{
					Title: "HiddenUntilCommit", Body: "tx-local body", Category: "tech",
				}); err != nil {
					return err
				}
				hits, err := den.NewQuery[FTSArticle](tx).Search(ctx, "HiddenUntilCommit")
				if err != nil {
					return err
				}
				insideHits = hits
				return nil // commit intentionally; we want the post-commit visibility check
			})
			require.NoError(t, err)
			assert.Empty(t, insideHits, "FTS bypasses the tx — tx-local docs stay invisible")

			after, err := den.NewQuery[FTSArticle](db).Search(ctx, "HiddenUntilCommit")
			require.NoError(t, err)
			require.Len(t, after, 1, "after commit the FTS index is updated and Search hits")
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
