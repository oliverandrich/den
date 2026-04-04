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

type AggProduct struct {
	document.Base
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Category string  `json:"category"`
}

func seedAggProducts(t *testing.T, db *den.DB) {
	t.Helper()
	ctx := context.Background()
	products := []*AggProduct{
		{Name: "A", Price: 10.0, Category: "X"},
		{Name: "B", Price: 20.0, Category: "X"},
		{Name: "C", Price: 30.0, Category: "Y"},
		{Name: "D", Price: 40.0, Category: "Y"},
		{Name: "E", Price: 50.0, Category: "Y"},
	}
	require.NoError(t, den.InsertMany(ctx, db, products))
}

func TestAvg(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	avg, err := den.NewQuery[AggProduct](ctx, db).Avg("price")
	require.NoError(t, err)
	assert.InDelta(t, 30.0, avg, 0.001) // (10+20+30+40+50)/5
}

func TestAvg_WithFilter(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	avg, err := den.NewQuery[AggProduct](ctx, db, where.Field("category").Eq("X")).Avg("price")
	require.NoError(t, err)
	assert.InDelta(t, 15.0, avg, 0.001) // (10+20)/2
}

func TestSum(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	sum, err := den.NewQuery[AggProduct](ctx, db).Sum("price")
	require.NoError(t, err)
	assert.InDelta(t, 150.0, sum, 0.001)
}

func TestMin(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	min, err := den.NewQuery[AggProduct](ctx, db).Min("price")
	require.NoError(t, err)
	assert.InDelta(t, 10.0, min, 0.001)
}

func TestMax(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	max, err := den.NewQuery[AggProduct](ctx, db).Max("price")
	require.NoError(t, err)
	assert.InDelta(t, 50.0, max, 0.001)
}

func TestAvg_Empty(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	ctx := context.Background()

	avg, err := den.NewQuery[AggProduct](ctx, db).Avg("price")
	require.NoError(t, err)
	assert.InDelta(t, 0.0, avg, 0.001)
}

func TestGroupBy(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	type CatStats struct {
		Category string  `den:"group_key"`
		AvgPrice float64 `den:"avg:price"`
		Total    float64 `den:"sum:price"`
		Count    int64   `den:"count"`
		MinPrice float64 `den:"min:price"`
		MaxPrice float64 `den:"max:price"`
	}

	var stats []CatStats
	err := den.NewQuery[AggProduct](ctx, db).GroupBy("category").Into(&stats)
	require.NoError(t, err)
	require.Len(t, stats, 2)

	// Find groups by category
	var x, y *CatStats
	for i := range stats {
		switch stats[i].Category {
		case "X":
			x = &stats[i]
		case "Y":
			y = &stats[i]
		}
	}

	require.NotNil(t, x)
	assert.Equal(t, int64(2), x.Count)
	assert.InDelta(t, 15.0, x.AvgPrice, 0.001)
	assert.InDelta(t, 30.0, x.Total, 0.001)
	assert.InDelta(t, 10.0, x.MinPrice, 0.001)
	assert.InDelta(t, 20.0, x.MaxPrice, 0.001)

	require.NotNil(t, y)
	assert.Equal(t, int64(3), y.Count)
	assert.InDelta(t, 40.0, y.AvgPrice, 0.001)
	assert.InDelta(t, 120.0, y.Total, 0.001)
	assert.InDelta(t, 30.0, y.MinPrice, 0.001)
	assert.InDelta(t, 50.0, y.MaxPrice, 0.001)
}

func TestProject(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	type Summary struct {
		Name  string  `json:"name"`
		Price float64 `json:"price"`
	}

	var summaries []Summary
	err := den.NewQuery[AggProduct](ctx, db).Sort("price", den.Asc).Project(&summaries)
	require.NoError(t, err)
	require.Len(t, summaries, 5)
	assert.Equal(t, "A", summaries[0].Name)
	assert.InDelta(t, 10.0, summaries[0].Price, 0.001)
}

func TestProject_WithFilter(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	type Summary struct {
		Name string `json:"name"`
	}

	var summaries []Summary
	err := den.NewQuery[AggProduct](ctx, db, where.Field("category").Eq("X")).Project(&summaries)
	require.NoError(t, err)
	assert.Len(t, summaries, 2)
}

func TestGroupBy_CacheHit(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	type CatStats struct {
		Category string `den:"group_key"`
		Count    int64  `den:"count"`
	}

	// First call — populates cache
	var stats1 []CatStats
	require.NoError(t, den.NewQuery[AggProduct](ctx, db).GroupBy("category").Into(&stats1))
	require.Len(t, stats1, 2)

	// Second call with same target type — should hit cache
	var stats2 []CatStats
	require.NoError(t, den.NewQuery[AggProduct](ctx, db).GroupBy("category").Into(&stats2))
	require.Len(t, stats2, 2)
}

func TestProject_InvalidTarget(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	// Not a pointer to slice — should error
	var single struct{ Name string }
	err := den.NewQuery[AggProduct](ctx, db).Project(&single)
	require.Error(t, err)
}

func TestQuerySet_Count(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	count, err := den.NewQuery[AggProduct](ctx, db, where.Field("category").Eq("Y")).Count()
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}
