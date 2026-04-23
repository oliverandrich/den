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
	Region   string  `json:"region,omitempty"`
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

	avg, err := den.NewQuery[AggProduct](db).Avg(ctx, "price")
	require.NoError(t, err)
	assert.InDelta(t, 30.0, avg, 0.001) // (10+20+30+40+50)/5
}

func TestAvg_WithFilter(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	avg, err := den.NewQuery[AggProduct](db, where.Field("category").Eq("X")).Avg(ctx, "price")
	require.NoError(t, err)
	assert.InDelta(t, 15.0, avg, 0.001) // (10+20)/2
}

func TestSum(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	sum, err := den.NewQuery[AggProduct](db).Sum(ctx, "price")
	require.NoError(t, err)
	assert.InDelta(t, 150.0, sum, 0.001)
}

func TestMin(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	min, err := den.NewQuery[AggProduct](db).Min(ctx, "price")
	require.NoError(t, err)
	assert.InDelta(t, 10.0, min, 0.001)
}

func TestMax(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	max, err := den.NewQuery[AggProduct](db).Max(ctx, "price")
	require.NoError(t, err)
	assert.InDelta(t, 50.0, max, 0.001)
}

// TestScalarAggregate_IgnoresLimitSkipSort pins that scalar aggregates
// (Avg / Sum / Min / Max) operate on the full WHERE-filtered set. The
// backend builder emits WHERE only, so Limit / Skip / Sort on the QuerySet
// have no effect on the returned scalar.
func TestScalarAggregate_IgnoresLimitSkipSort(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	qs := den.NewQuery[AggProduct](db).Limit(2).Skip(1).Sort("price", den.Desc)

	sum, err := qs.Sum(ctx, "price")
	require.NoError(t, err)
	assert.InDelta(t, 150.0, sum, 0.001, "Sum must operate on the full filtered set")

	avg, err := qs.Avg(ctx, "price")
	require.NoError(t, err)
	assert.InDelta(t, 30.0, avg, 0.001)

	min, err := qs.Min(ctx, "price")
	require.NoError(t, err)
	assert.InDelta(t, 10.0, min, 0.001)

	max, err := qs.Max(ctx, "price")
	require.NoError(t, err)
	assert.InDelta(t, 50.0, max, 0.001)
}

func TestAvg_Empty(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	ctx := context.Background()

	avg, err := den.NewQuery[AggProduct](db).Avg(ctx, "price")
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
	err := den.NewQuery[AggProduct](db).GroupBy("category").Into(ctx, &stats)
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

// TestGroupBy_MultiKey pins two-key GROUP BY: callers pass two fields to
// GroupBy and the target struct carries positional group_key:N tags.
func TestGroupBy_MultiKey(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	ctx := context.Background()

	// Seed with (category, region) pairs so (X, north) has 2 rows,
	// (X, south) has 1, (Y, north) has 1.
	products := []AggProduct{
		{Name: "a", Price: 10, Category: "X", Region: "north"},
		{Name: "b", Price: 20, Category: "X", Region: "north"},
		{Name: "c", Price: 30, Category: "X", Region: "south"},
		{Name: "d", Price: 40, Category: "Y", Region: "north"},
	}
	for i := range products {
		require.NoError(t, den.Insert(ctx, db, &products[i]))
	}

	type Stats struct {
		Category string  `den:"group_key:0"`
		Region   string  `den:"group_key:1"`
		Count    int64   `den:"count"`
		Total    float64 `den:"sum:price"`
	}

	var stats []Stats
	err := den.NewQuery[AggProduct](db).GroupBy("category", "region").Into(ctx, &stats)
	require.NoError(t, err)
	require.Len(t, stats, 3)

	byKey := map[string]Stats{}
	for _, s := range stats {
		byKey[s.Category+"|"+s.Region] = s
	}

	require.Contains(t, byKey, "X|north")
	assert.Equal(t, int64(2), byKey["X|north"].Count)
	assert.InDelta(t, 30.0, byKey["X|north"].Total, 0.001)

	require.Contains(t, byKey, "X|south")
	assert.Equal(t, int64(1), byKey["X|south"].Count)
	assert.InDelta(t, 30.0, byKey["X|south"].Total, 0.001)

	require.Contains(t, byKey, "Y|north")
	assert.Equal(t, int64(1), byKey["Y|north"].Count)
	assert.InDelta(t, 40.0, byKey["Y|north"].Total, 0.001)
}

// TestGroupBy_SingleKeyUnindexedTag confirms the legacy `den:"group_key"`
// tag still works as slot 0 when exactly one group key is requested.
func TestGroupBy_SingleKeyUnindexedTag(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	type Stats struct {
		Category string `den:"group_key"`
		Count    int64  `den:"count"`
	}

	var stats []Stats
	err := den.NewQuery[AggProduct](db).GroupBy("category").Into(ctx, &stats)
	require.NoError(t, err)
	assert.Len(t, stats, 2)
}

// TestGroupBy_MissingKeyTag rejects targets whose group_key tag count does
// not match the number of group fields.
func TestGroupBy_MissingKeyTag(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	ctx := context.Background()

	// Two fields requested, only one slot tagged — should error.
	type Stats struct {
		Category string `den:"group_key:0"`
		Count    int64  `den:"count"`
	}

	var stats []Stats
	err := den.NewQuery[AggProduct](db).GroupBy("category", "region").Into(ctx, &stats)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "group_key")
}

// TestGroupBy_MixedTagForms rejects targets that mix `group_key` (unindexed)
// with `group_key:N` (positional) — only one style per target.
func TestGroupBy_MixedTagForms(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	ctx := context.Background()

	type Stats struct {
		Category string `den:"group_key"`
		Region   string `den:"group_key:1"`
		Count    int64  `den:"count"`
	}

	var stats []Stats
	err := den.NewQuery[AggProduct](db).GroupBy("category", "region").Into(ctx, &stats)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "group_key")
}

// TestGroupBy_DuplicateSlot rejects targets with two fields claiming the
// same positional slot.
func TestGroupBy_DuplicateSlot(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	ctx := context.Background()

	type Stats struct {
		A     string `den:"group_key:0"`
		B     string `den:"group_key:0"`
		Count int64  `den:"count"`
	}

	var stats []Stats
	err := den.NewQuery[AggProduct](db).GroupBy("category", "region").Into(ctx, &stats)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "group_key")
}

// TestGroupBy_SortByKey orders grouped results by a group key.
func TestGroupBy_SortByKey(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	type Stats struct {
		Category string `den:"group_key"`
		Count    int64  `den:"count"`
	}

	var asc []Stats
	err := den.NewQuery[AggProduct](db).Sort("category", den.Asc).
		GroupBy("category").Into(ctx, &asc)
	require.NoError(t, err)
	require.Len(t, asc, 2)
	assert.Equal(t, "X", asc[0].Category)
	assert.Equal(t, "Y", asc[1].Category)

	var desc []Stats
	err = den.NewQuery[AggProduct](db).Sort("category", den.Desc).
		GroupBy("category").Into(ctx, &desc)
	require.NoError(t, err)
	require.Len(t, desc, 2)
	assert.Equal(t, "Y", desc[0].Category)
	assert.Equal(t, "X", desc[1].Category)
}

// TestGroupBy_SortByAgg orders grouped results by an aggregate via OrderByAgg.
func TestGroupBy_SortByAgg(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	type Stats struct {
		Category string `den:"group_key"`
		Count    int64  `den:"count"`
	}

	var stats []Stats
	err := den.NewQuery[AggProduct](db).
		GroupBy("category").
		OrderByAgg(den.OpCount, "", den.Desc).
		Into(ctx, &stats)
	require.NoError(t, err)
	require.Len(t, stats, 2)
	// Y has 3 rows, X has 2 — DESC by COUNT(*) places Y first.
	assert.Equal(t, "Y", stats[0].Category)
	assert.Equal(t, int64(3), stats[0].Count)
	assert.Equal(t, "X", stats[1].Category)
	assert.Equal(t, int64(2), stats[1].Count)
}

// TestGroupBy_Limit caps the number of group rows returned.
func TestGroupBy_Limit(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	type Stats struct {
		Category string `den:"group_key"`
		Count    int64  `den:"count"`
	}

	var stats []Stats
	err := den.NewQuery[AggProduct](db).Sort("category", den.Asc).Limit(1).
		GroupBy("category").Into(ctx, &stats)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, "X", stats[0].Category)
}

// TestGroupBy_SortByNonKey_Error rejects a Sort field that is neither a
// group key nor an aggregate — callers must use OrderByAgg for aggregates.
func TestGroupBy_SortByNonKey_Error(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	ctx := context.Background()

	type Stats struct {
		Category string `den:"group_key"`
		Count    int64  `den:"count"`
	}

	var stats []Stats
	err := den.NewQuery[AggProduct](db).Sort("price", den.Asc).
		GroupBy("category").Into(ctx, &stats)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "price")
	assert.Contains(t, err.Error(), "group key")
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
	err := den.NewQuery[AggProduct](db).Sort("price", den.Asc).Project(ctx, &summaries)
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
	err := den.NewQuery[AggProduct](db, where.Field("category").Eq("X")).Project(ctx, &summaries)
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
	require.NoError(t, den.NewQuery[AggProduct](db).GroupBy("category").Into(ctx, &stats1))
	require.Len(t, stats1, 2)

	// Second call with same target type — should hit cache
	var stats2 []CatStats
	require.NoError(t, den.NewQuery[AggProduct](db).GroupBy("category").Into(ctx, &stats2))
	require.Len(t, stats2, 2)
}

func TestProject_InvalidTarget(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	// Not a pointer to slice — should error
	var single struct{ Name string }
	err := den.NewQuery[AggProduct](db).Project(ctx, &single)
	require.Error(t, err)
}

func TestQuerySet_Count(t *testing.T) {
	db := dentest.MustOpen(t, &AggProduct{})
	seedAggProducts(t, db)
	ctx := context.Background()

	count, err := den.NewQuery[AggProduct](db, where.Field("category").Eq("Y")).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}
