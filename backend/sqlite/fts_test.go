package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/where"
)

func TestEnsureFTS(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "articles", den.CollectionMeta{}))

	fts := b.(den.FTSProvider)
	err := fts.EnsureFTS(ctx, "articles", []string{"title", "body"})
	assert.NoError(t, err)
}

func TestSearch_Basic(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "articles", den.CollectionMeta{}))
	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "articles", []string{"title", "body"}))

	require.NoError(t, b.Put(ctx, "articles", "a1", []byte(`{"title":"Go Programming","body":"Learn Go"}`)))
	require.NoError(t, b.Put(ctx, "articles", "a2", []byte(`{"title":"Python Basics","body":"Learn Python"}`)))

	iter, err := fts.Search(ctx, "articles", "Go", &den.Query{Collection: "articles"})
	require.NoError(t, err)
	defer iter.Close()

	var ids []string
	for iter.Next() {
		ids = append(ids, iter.ID())
	}
	require.NoError(t, iter.Err())
	assert.Equal(t, []string{"a1"}, ids)
}

func TestSearch_NoResults(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "articles", den.CollectionMeta{}))
	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "articles", []string{"title"}))

	require.NoError(t, b.Put(ctx, "articles", "a1", []byte(`{"title":"Hello World"}`)))

	iter, err := fts.Search(ctx, "articles", "nonexistent", &den.Query{Collection: "articles"})
	require.NoError(t, err)
	defer iter.Close()

	assert.False(t, iter.Next())
}

func TestSearch_WithLimit(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "articles", den.CollectionMeta{}))
	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "articles", []string{"title"}))

	require.NoError(t, b.Put(ctx, "articles", "a1", []byte(`{"title":"Go one"}`)))
	require.NoError(t, b.Put(ctx, "articles", "a2", []byte(`{"title":"Go two"}`)))
	require.NoError(t, b.Put(ctx, "articles", "a3", []byte(`{"title":"Go three"}`)))

	iter, err := fts.Search(ctx, "articles", "Go", &den.Query{Collection: "articles", LimitN: 2})
	require.NoError(t, err)
	defer iter.Close()

	count := 0
	for iter.Next() {
		count++
	}
	assert.Equal(t, 2, count)
}

func TestSearch_WithSkip(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "articles", den.CollectionMeta{}))
	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "articles", []string{"title"}))

	require.NoError(t, b.Put(ctx, "articles", "a1", []byte(`{"title":"Go one"}`)))
	require.NoError(t, b.Put(ctx, "articles", "a2", []byte(`{"title":"Go two"}`)))
	require.NoError(t, b.Put(ctx, "articles", "a3", []byte(`{"title":"Go three"}`)))

	q := &den.Query{Collection: "articles", SkipN: 1}
	iter, err := fts.Search(ctx, "articles", "Go", q)
	require.NoError(t, err)
	defer iter.Close()

	count := 0
	for iter.Next() {
		count++
	}
	assert.Equal(t, 2, count)
}

func TestSearch_WithSort(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "articles", den.CollectionMeta{}))
	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "articles", []string{"title"}))

	require.NoError(t, b.Put(ctx, "articles", "a1", []byte(`{"title":"Go Zebra"}`)))
	require.NoError(t, b.Put(ctx, "articles", "a2", []byte(`{"title":"Go Alpha"}`)))

	q := &den.Query{
		Collection: "articles",
		SortFields: []den.SortEntry{{Field: "title", Dir: den.Asc}},
	}
	iter, err := fts.Search(ctx, "articles", "Go", q)
	require.NoError(t, err)
	defer iter.Close()

	var ids []string
	for iter.Next() {
		ids = append(ids, iter.ID())
	}
	assert.Equal(t, []string{"a2", "a1"}, ids)
}

func TestSearch_UpdatedDoc(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "articles", den.CollectionMeta{}))
	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "articles", []string{"title"}))

	require.NoError(t, b.Put(ctx, "articles", "a1", []byte(`{"title":"Go Programming"}`)))

	// Update title
	require.NoError(t, b.Put(ctx, "articles", "a1", []byte(`{"title":"Rust Programming"}`)))

	// Search for old term should not find it
	iter, err := fts.Search(ctx, "articles", "Go", &den.Query{Collection: "articles"})
	require.NoError(t, err)
	assert.False(t, iter.Next())
	iter.Close()

	// Search for new term should find it
	iter, err = fts.Search(ctx, "articles", "Rust", &den.Query{Collection: "articles"})
	require.NoError(t, err)
	assert.True(t, iter.Next())
	assert.Equal(t, "a1", iter.ID())
	iter.Close()
}

func TestSearch_WithCondition(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "articles", den.CollectionMeta{}))
	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "articles", []string{"title", "body"}))

	require.NoError(t, b.Put(ctx, "articles", "a1", []byte(`{"title":"Go Web","body":"Building web apps","category":"tech"}`)))
	require.NoError(t, b.Put(ctx, "articles", "a2", []byte(`{"title":"Go Cook","body":"Go to recipes","category":"food"}`)))

	q := &den.Query{
		Collection: "articles",
		Conditions: []where.Condition{where.Field("category").Eq("tech")},
	}
	iter, err := fts.Search(ctx, "articles", "Go", q)
	require.NoError(t, err)
	defer iter.Close()

	var ids []string
	for iter.Next() {
		ids = append(ids, iter.ID())
	}
	assert.Equal(t, []string{"a1"}, ids)
}

func TestSearch_DeletedDoc(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "articles", den.CollectionMeta{}))
	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "articles", []string{"title"}))

	require.NoError(t, b.Put(ctx, "articles", "a1", []byte(`{"title":"Go Programming"}`)))
	require.NoError(t, b.Delete(ctx, "articles", "a1"))

	iter, err := fts.Search(ctx, "articles", "Go", &den.Query{Collection: "articles"})
	require.NoError(t, err)
	assert.False(t, iter.Next())
	iter.Close()
}
