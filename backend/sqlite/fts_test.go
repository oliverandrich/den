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

// searchIDs runs an FTS search over the articles collection and collects
// the matching ids in iteration order.
func searchIDs(t *testing.T, fts den.FTSProvider, term string) []string {
	t.Helper()
	ctx := context.Background()

	iter, err := fts.Search(ctx, "articles", term, &den.Query{Collection: "articles"})
	require.NoError(t, err)
	defer iter.Close()

	var ids []string
	for iter.Next() {
		ids = append(ids, iter.ID())
	}
	require.NoError(t, iter.Err())
	return ids
}

func TestEnsureFTS_BackfillsExistingRows(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "articles", den.CollectionMeta{}))

	// Rows exist before FTS is enabled — the schema-upgrade scenario.
	require.NoError(t, b.Put(ctx, "articles", "a1", []byte(`{"title":"Go Programming","body":"Learn Go"}`)))
	require.NoError(t, b.Put(ctx, "articles", "a2", []byte(`{"title":"Python Basics","body":"Learn Python"}`)))

	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "articles", []string{"title", "body"}))

	assert.Equal(t, []string{"a1"}, searchIDs(t, fts, "Go"))
}

func TestEnsureFTS_SecondCallNoErrorNoDuplicates(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "articles", den.CollectionMeta{}))
	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "articles", []string{"title"}))

	require.NoError(t, b.Put(ctx, "articles", "a1", []byte(`{"title":"Go Programming"}`)))

	// Re-registering must not error and must not re-backfill rows the
	// triggers already indexed.
	require.NoError(t, fts.EnsureFTS(ctx, "articles", []string{"title"}))

	assert.Equal(t, []string{"a1"}, searchIDs(t, fts, "Go"))
}

func TestEnsureFTS_BackfillsNestedField(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "articles", den.CollectionMeta{}))

	require.NoError(t, b.Put(ctx, "articles", "a1", []byte(`{"profile":{"bio":"mechanical computation"}}`)))

	fts := b.(den.FTSProvider)
	require.NoError(t, fts.EnsureFTS(ctx, "articles", []string{"profile.bio"}))

	assert.Equal(t, []string{"a1"}, searchIDs(t, fts, "mechanical"))
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
