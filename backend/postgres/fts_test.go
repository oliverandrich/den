//go:build postgres

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
