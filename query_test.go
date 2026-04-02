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

type QueryProduct struct {
	document.Base
	Name     string  `json:"name" den:"index"`
	Price    float64 `json:"price" den:"index"`
	Category string  `json:"category"`
}

func seedQueryProducts(t *testing.T, db *den.DB) {
	t.Helper()
	ctx := context.Background()
	products := []QueryProduct{
		{Name: "Alpha", Price: 10.0, Category: "A"},
		{Name: "Beta", Price: 20.0, Category: "B"},
		{Name: "Gamma", Price: 30.0, Category: "A"},
		{Name: "Delta", Price: 15.0, Category: "B"},
		{Name: "Epsilon", Price: 25.0, Category: "A"},
	}
	for i := range products {
		require.NoError(t, den.Insert(ctx, db, &products[i]))
	}
}

func TestFind_All(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](ctx, db).All()
	require.NoError(t, err)
	assert.Len(t, results, 5)
}

func TestFind_WithCondition(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](ctx, db, where.Field("category").Eq("A")).All()
	require.NoError(t, err)
	assert.Len(t, results, 3)
	for _, r := range results {
		assert.Equal(t, "A", r.Category)
	}
}

func TestFind_SortAsc(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](ctx, db).Sort("price", den.Asc).All()
	require.NoError(t, err)
	require.Len(t, results, 5)
	assert.InDelta(t, 10.0, results[0].Price, 0.001)
	assert.InDelta(t, 30.0, results[4].Price, 0.001)
}

func TestFind_SortDesc(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](ctx, db).Sort("price", den.Desc).All()
	require.NoError(t, err)
	require.Len(t, results, 5)
	assert.InDelta(t, 30.0, results[0].Price, 0.001)
	assert.InDelta(t, 10.0, results[4].Price, 0.001)
}

func TestFind_Limit(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](ctx, db).Sort("price", den.Asc).Limit(3).All()
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestFind_Skip(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](ctx, db).Sort("price", den.Asc).Skip(2).All()
	require.NoError(t, err)
	assert.Len(t, results, 3)
	assert.InDelta(t, 20.0, results[0].Price, 0.001)
}

func TestFind_RangeCondition(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](ctx, db,
		where.Field("price").Gte(15.0),
		where.Field("price").Lte(25.0),
	).All()
	require.NoError(t, err)
	assert.Len(t, results, 3) // 15, 20, 25
}

func TestFindOne(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	p, err := den.NewQuery[QueryProduct](ctx, db, where.Field("name").Eq("Beta")).First()
	require.NoError(t, err)
	assert.Equal(t, "Beta", p.Name)
}

func TestFindOne_NotFound(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	_, err := den.NewQuery[QueryProduct](ctx, db, where.Field("name").Eq("Nonexistent")).First()
	require.ErrorIs(t, err, den.ErrNotFound)
}

func TestFindAll(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](ctx, db).All()
	require.NoError(t, err)
	assert.Len(t, results, 5)
}

func TestCount(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	count, err := den.NewQuery[QueryProduct](ctx, db, where.Field("category").Eq("A")).Count()
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestCount_All(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	count, err := den.NewQuery[QueryProduct](ctx, db).Count()
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)
}

func TestExists(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	exists, err := den.NewQuery[QueryProduct](ctx, db, where.Field("name").Eq("Alpha")).Exists()
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = den.NewQuery[QueryProduct](ctx, db, where.Field("name").Eq("Nonexistent")).Exists()
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestFind_CursorPagination(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	ctx := context.Background()

	p := &QueryProduct{Name: "Solo", Price: 1.0}
	require.NoError(t, den.Insert(ctx, db, p))

	// After a very high ID should return nothing
	results, err := den.NewQuery[QueryProduct](ctx, db).After("ZZZZZZZZZZZZZZZZZZZZZZZZZZ").All()
	require.NoError(t, err)
	assert.Empty(t, results)

	// Before a very low ID should return nothing
	results, err = den.NewQuery[QueryProduct](ctx, db).Before("00000000000000000000000000").All()
	require.NoError(t, err)
	assert.Empty(t, results)

	// After a very low ID should return all
	results, err = den.NewQuery[QueryProduct](ctx, db).After("00000000000000000000000000").All()
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestFindWithCount(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, total, err := den.NewQuery[QueryProduct](ctx, db, where.Field("category").Eq("A")).
		Sort("price", den.Asc).
		Limit(2).
		AllWithCount()
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, results, 2)
}

// --- QuerySet chain API tests ---

func TestQuerySet_All(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](ctx, db).All()
	require.NoError(t, err)
	assert.Len(t, results, 5)
}

func TestQuerySet_Where(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](ctx, db).
		Where(where.Field("category").Eq("A")).
		All()
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestQuerySet_SortAndLimit(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](ctx, db).
		Sort("price", den.Asc).
		Limit(2).
		All()
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.InDelta(t, 10.0, results[0].Price, 0.001)
	assert.InDelta(t, 15.0, results[1].Price, 0.001)
}

func TestQuerySet_First(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	p, err := den.NewQuery[QueryProduct](ctx, db).
		Where(where.Field("name").Eq("Beta")).
		First()
	require.NoError(t, err)
	assert.Equal(t, "Beta", p.Name)
}

func TestQuerySet_CountChain(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	count, err := den.NewQuery[QueryProduct](ctx, db).
		Where(where.Field("category").Eq("A")).
		Count()
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestQuerySet_Exists(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	exists, err := den.NewQuery[QueryProduct](ctx, db).
		Where(where.Field("name").Eq("Alpha")).
		Exists()
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = den.NewQuery[QueryProduct](ctx, db).
		Where(where.Field("name").Eq("Nonexistent")).
		Exists()
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestQuerySet_AllWithCount(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, total, err := den.NewQuery[QueryProduct](ctx, db).
		Where(where.Field("category").Eq("A")).
		Sort("price", den.Asc).
		Limit(2).
		AllWithCount()
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, results, 2)
}

func TestQuerySet_Lazy(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	// Base query — no execution yet
	base := den.NewQuery[QueryProduct](ctx, db).
		Where(where.Field("category").Eq("A"))

	// Derive two different queries from the same base
	sorted, err := base.Sort("price", den.Asc).All()
	require.NoError(t, err)
	assert.InDelta(t, 10.0, sorted[0].Price, 0.001)

	count, err := base.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

// --- Update terminal method tests ---

func TestQuerySet_Update(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	count, err := den.NewQuery[QueryProduct](ctx, db, where.Field("category").Eq("A")).
		Update(den.SetFields{"category": "Z"})
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)

	// Verify the update
	results, err := den.NewQuery[QueryProduct](ctx, db, where.Field("category").Eq("Z")).All()
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Original category B should be untouched
	results, err = den.NewQuery[QueryProduct](ctx, db, where.Field("category").Eq("B")).All()
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestQuerySet_Update_NoMatches(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	ctx := context.Background()

	count, err := den.NewQuery[QueryProduct](ctx, db, where.Field("name").Eq("Nonexistent")).
		Update(den.SetFields{"price": 99.0})
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestQuerySet_Update_InvalidField(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	_, err := den.NewQuery[QueryProduct](ctx, db).
		Update(den.SetFields{"nonexistent": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field")
}

func TestQuerySet_Update_TypeMismatch(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	_, err := den.NewQuery[QueryProduct](ctx, db).
		Update(den.SetFields{"price": "not-a-float"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot assign")
}

func TestQuerySet_Update_NilValue(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	// nil value should set the zero value, not panic
	_, err := den.NewQuery[QueryProduct](ctx, db, where.Field("name").Eq("Alpha")).
		Update(den.SetFields{"category": nil})
	require.NoError(t, err)

	results, err := den.NewQuery[QueryProduct](ctx, db, where.Field("name").Eq("Alpha")).All()
	require.NoError(t, err)
	for _, r := range results {
		assert.Empty(t, r.Category)
	}
}
