package den_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/where"
)

type Event struct {
	document.Base
	Name     string  `json:"name"`
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Category string  `json:"category"`
	Priority int     `json:"priority"`
}

func seedEvents(t *testing.T, db *den.DB) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, den.InsertMany(ctx, db, []*Event{
		{Name: "Alpha Meeting", Start: 10, End: 20, Category: "work", Priority: 1},
		{Name: "Beta Review", Start: 15, End: 25, Category: "work", Priority: 2},
		{Name: "Gamma Party", Start: 30, End: 40, Category: "social", Priority: 3},
		{Name: "Delta Standup", Start: 5, End: 8, Category: "work", Priority: 1},
	}))
}

type DocWithMap struct {
	document.Base
	Name     string         `json:"name"`
	Metadata map[string]any `json:"metadata"`
}

// --- HasKey ---

func TestHasKey(t *testing.T) {
	db := dentest.MustOpen(t, &DocWithMap{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*DocWithMap{
		{Name: "A", Metadata: map[string]any{"color": "red", "size": "large"}},
		{Name: "B", Metadata: map[string]any{"weight": 10}},
		{Name: "C", Metadata: map[string]any{"color": "blue"}},
	}))

	results, err := den.NewQuery[DocWithMap](db, where.Field("metadata").HasKey("color")).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestHasKey_NoMatch(t *testing.T) {
	db := dentest.MustOpen(t, &DocWithMap{})
	ctx := context.Background()

	require.NoError(t, den.Insert(ctx, db, &DocWithMap{
		Name: "A", Metadata: map[string]any{"size": "large"},
	}))

	results, err := den.NewQuery[DocWithMap](db, where.Field("metadata").HasKey("color")).All(ctx)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// --- Multi-Field Sort ---

func TestMultiSort(t *testing.T) {
	db := dentest.MustOpen(t, &Event{})
	seedEvents(t, db)
	ctx := context.Background()

	// Sort by category ASC, then priority DESC
	results, err := den.NewQuery[Event](db).
		Sort("category", den.Asc).
		Sort("priority", den.Desc).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 4)

	// social first (asc), then work
	assert.Equal(t, "social", results[0].Category)
	// within work: priority 2, then 1, 1
	assert.Equal(t, "work", results[1].Category)
	assert.Equal(t, 2, results[1].Priority)
}

// --- RegExp ---

func TestRegExp(t *testing.T) {
	db := dentest.MustOpen(t, &Event{})
	seedEvents(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[Event](db, where.Field("name").RegExp(".*Meeting.*")).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Alpha Meeting", results[0].Name)
}

func TestRegExp_Compiled(t *testing.T) {
	db := dentest.MustOpen(t, &Event{})
	seedEvents(t, db)
	ctx := context.Background()

	re := regexp.MustCompile(`^(Alpha|Delta)`)
	results, err := den.NewQuery[Event](db, where.Field("name").RegExp(re)).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestRegExp_NoMatch(t *testing.T) {
	db := dentest.MustOpen(t, &Event{})
	seedEvents(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[Event](db, where.Field("name").RegExp("^Nonexistent")).All(ctx)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// --- Field-vs-Field ---

func TestFieldRef(t *testing.T) {
	db := dentest.MustOpen(t, &Event{})
	seedEvents(t, db)
	ctx := context.Background()

	// Find events where end - start > 15 (long events)
	// We can't do arithmetic, but we can compare: end > start + threshold
	// Simple case: find events where end > 20
	results, err := den.NewQuery[Event](db, where.Field("end").Gt(where.FieldRef("start"))).All(ctx)
	require.NoError(t, err)
	// All events have end > start
	assert.Len(t, results, 4)
}

func TestFieldRef_Filtered(t *testing.T) {
	db := dentest.MustOpen(t, &Event{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*Event{
		{Name: "Valid", Start: 10, End: 20},
		{Name: "Invalid", Start: 30, End: 20}, // end < start
		{Name: "Equal", Start: 15, End: 15},   // end == start
	}))

	results, err := den.NewQuery[Event](db, where.Field("end").Gt(where.FieldRef("start"))).All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Valid", results[0].Name)
}

func TestFieldRef_Eq(t *testing.T) {
	db := dentest.MustOpen(t, &Event{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*Event{
		{Name: "Same", Start: 15, End: 15},
		{Name: "Different", Start: 10, End: 20},
	}))

	results, err := den.NewQuery[Event](db, where.Field("end").Eq(where.FieldRef("start"))).All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Same", results[0].Name)
}
