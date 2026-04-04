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

	results, err := den.NewQuery[FTSArticle](ctx, db).Search("Go")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Go Programming", results[0].Title)
}

func TestSearch_SQLite_MultipleResults(t *testing.T) {
	db := dentest.MustOpen(t, &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*FTSArticle{
		{Title: "Go Basics", Body: "Learn Go", Category: "tech"},
		{Title: "Advanced Go", Body: "Go concurrency patterns", Category: "tech"},
		{Title: "Python Basics", Body: "Learn Python", Category: "tech"},
	}))

	results, err := den.NewQuery[FTSArticle](ctx, db).Search("Go")
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

	results, err := den.NewQuery[FTSArticle](ctx, db, where.Field("category").Eq("tech")).Search("Go")
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

	results, err := den.NewQuery[FTSArticle](ctx, db).Limit(2).Search("Go")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestSearch_SQLite_NoResults(t *testing.T) {
	db := dentest.MustOpen(t, &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, den.Insert(ctx, db, &FTSArticle{Title: "Hello", Body: "World"}))

	results, err := den.NewQuery[FTSArticle](ctx, db).Search("nonexistent")
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

	results, err := den.NewQuery[FTSArticle](ctx, db).Search("programming")
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

	results, err := den.NewQuery[FTSArticle](ctx, db, where.Field("category").Eq("tech")).Search("Go")
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

	results, err := den.NewQuery[FTSArticle](ctx, db).Limit(2).Search("Go")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestSearch_Postgres_NoResults(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, den.Insert(ctx, db, &FTSArticle{Title: "Hello", Body: "World"}))

	results, err := den.NewQuery[FTSArticle](ctx, db).Search("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, results)
}
