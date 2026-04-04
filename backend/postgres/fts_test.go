package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
)

func TestEnsureFTS(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_fts_articles", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_fts_articles") })

	fts := b.(den.FTSProvider)
	err := fts.EnsureFTS(ctx, "test_fts_articles", []string{"title", "body"})
	assert.NoError(t, err)
}

func TestSearch_PG(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_fts_search", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_fts_search") })

	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "test_fts_search", []string{"title", "body"}))

	require.NoError(t, b.Put(ctx, "test_fts_search", "a1", []byte(`{"title":"Go Programming","body":"Learn Go language"}`)))
	require.NoError(t, b.Put(ctx, "test_fts_search", "a2", []byte(`{"title":"Python Tutorial","body":"Learn Python basics"}`)))
	require.NoError(t, b.Put(ctx, "test_fts_search", "a3", []byte(`{"title":"Cooking Guide","body":"How to cook pasta"}`)))

	iter, err := fts.Search(ctx, "test_fts_search", "programming", &den.Query{Collection: "test_fts_search"})
	require.NoError(t, err)
	defer iter.Close()

	var ids []string
	for iter.Next() {
		ids = append(ids, iter.ID())
	}
	require.NoError(t, iter.Err())
	assert.Contains(t, ids, "a1")
}

func TestSearch_PG_NoConditions(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_fts_nc", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_fts_nc") })

	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "test_fts_nc", []string{"title"}))

	require.NoError(t, b.Put(ctx, "test_fts_nc", "a1", []byte(`{"title":"Go programming"}`)))
	require.NoError(t, b.Put(ctx, "test_fts_nc", "a2", []byte(`{"title":"Python basics"}`)))

	iter, err := fts.Search(ctx, "test_fts_nc", "Go", &den.Query{Collection: "test_fts_nc"})
	require.NoError(t, err)
	defer iter.Close()

	count := 0
	for iter.Next() {
		count++
	}
	require.NoError(t, iter.Err())
	assert.Equal(t, 1, count)
}

func TestSearch_PG_WithSort(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_fts_sort", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_fts_sort") })

	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "test_fts_sort", []string{"title"}))

	require.NoError(t, b.Put(ctx, "test_fts_sort", "a1", []byte(`{"title":"Go advanced"}`)))
	require.NoError(t, b.Put(ctx, "test_fts_sort", "a2", []byte(`{"title":"Go basics"}`)))

	q := &den.Query{
		Collection: "test_fts_sort",
		SortFields: []den.SortEntry{{Field: "title", Dir: den.Asc}},
	}
	iter, err := fts.Search(ctx, "test_fts_sort", "Go", q)
	require.NoError(t, err)
	defer iter.Close()

	var ids []string
	for iter.Next() {
		ids = append(ids, iter.ID())
	}
	require.NoError(t, iter.Err())
	assert.Len(t, ids, 2)
	assert.Equal(t, "a1", ids[0]) // "advanced" < "basics"
}

func TestSearch_PG_WithSkip(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_fts_skip", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_fts_skip") })

	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "test_fts_skip", []string{"title"}))

	require.NoError(t, b.Put(ctx, "test_fts_skip", "a1", []byte(`{"title":"Go one"}`)))
	require.NoError(t, b.Put(ctx, "test_fts_skip", "a2", []byte(`{"title":"Go two"}`)))
	require.NoError(t, b.Put(ctx, "test_fts_skip", "a3", []byte(`{"title":"Go three"}`)))

	q := &den.Query{Collection: "test_fts_skip", SkipN: 1}
	iter, err := fts.Search(ctx, "test_fts_skip", "Go", q)
	require.NoError(t, err)
	defer iter.Close()

	count := 0
	for iter.Next() {
		count++
	}
	assert.Equal(t, 2, count)
}

func TestSearch_PG_WithLimit(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_fts_limit", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_fts_limit") })

	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "test_fts_limit", []string{"title"}))

	require.NoError(t, b.Put(ctx, "test_fts_limit", "a1", []byte(`{"title":"Go one"}`)))
	require.NoError(t, b.Put(ctx, "test_fts_limit", "a2", []byte(`{"title":"Go two"}`)))
	require.NoError(t, b.Put(ctx, "test_fts_limit", "a3", []byte(`{"title":"Go three"}`)))

	iter, err := fts.Search(ctx, "test_fts_limit", "Go", &den.Query{Collection: "test_fts_limit", LimitN: 2})
	require.NoError(t, err)
	defer iter.Close()

	count := 0
	for iter.Next() {
		count++
	}
	assert.Equal(t, 2, count)
}
