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

func TestIter(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*Product{
		{Name: "A", Price: 1.0},
		{Name: "B", Price: 2.0},
		{Name: "C", Price: 3.0},
	}))

	var names []string
	for p, err := range den.NewQuery[Product](db).Iter(ctx) {
		require.NoError(t, err)
		names = append(names, p.Name)
	}
	assert.Len(t, names, 3)
}

func TestIter_Empty(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	count := 0
	for _, err := range den.NewQuery[Product](db).Iter(ctx) {
		require.NoError(t, err)
		count++
	}
	assert.Equal(t, 0, count)
}

func TestIter_Break(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*Product{
		{Name: "A", Price: 1.0},
		{Name: "B", Price: 2.0},
		{Name: "C", Price: 3.0},
	}))

	count := 0
	for _, err := range den.NewQuery[Product](db).Iter(ctx) {
		require.NoError(t, err)
		count++
		if count == 1 {
			break
		}
	}
	assert.Equal(t, 1, count)
}

func TestIter_ExcludesSoftDeleted(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	products := []*SoftProduct{
		{Name: "Keep", Price: 10.0},
		{Name: "Delete", Price: 20.0},
	}
	require.NoError(t, den.InsertMany(ctx, db, products))
	require.NoError(t, den.Delete(ctx, db, products[1]))

	var names []string
	for p, err := range den.NewQuery[SoftProduct](db).Iter(ctx) {
		require.NoError(t, err)
		names = append(names, p.Name)
	}
	assert.Equal(t, []string{"Keep"}, names)
}

type IterLinkedDoc struct {
	document.Base
	Name string                `json:"name"`
	Ref  den.Link[IterRefItem] `json:"ref"`
}

type IterRefItem struct {
	document.Base
	Label string `json:"label"`
}

func TestIter_WithFetchLinks(t *testing.T) {
	db := dentest.MustOpen(t, &IterLinkedDoc{}, &IterRefItem{})
	ctx := context.Background()

	ref := &IterRefItem{Label: "Target"}
	require.NoError(t, den.Insert(ctx, db, ref))

	docs := []*IterLinkedDoc{
		{Name: "A", Ref: den.NewLink(ref)},
		{Name: "B", Ref: den.NewLink(ref)},
	}
	require.NoError(t, den.InsertMany(ctx, db, docs))

	count := 0
	for d, err := range den.NewQuery[IterLinkedDoc](db).WithFetchLinks().Iter(ctx) {
		require.NoError(t, err)
		require.True(t, d.Ref.IsLoaded())
		assert.Equal(t, "Target", d.Ref.Value.Label)
		count++
	}
	assert.Equal(t, 2, count)
}

func TestIter_TerminatesOnFetchLinksError(t *testing.T) {
	db := dentest.MustOpen(t, &IterLinkedDoc{}, &IterRefItem{})
	ctx := context.Background()

	ref := &IterRefItem{Label: "ok"}
	require.NoError(t, den.Insert(ctx, db, ref))

	// Seed three rows with known IDs so iteration visits them in order.
	// The middle row has a dangling link; the third must never be yielded.
	good := &IterLinkedDoc{Name: "A", Ref: den.NewLink(ref)}
	good.ID = "01-good"
	bad := &IterLinkedDoc{Name: "B", Ref: den.Link[IterRefItem]{ID: "does-not-exist"}}
	bad.ID = "02-bad"
	last := &IterLinkedDoc{Name: "C", Ref: den.NewLink(ref)}
	last.ID = "03-last"

	require.NoError(t, den.Insert(ctx, db, good))
	require.NoError(t, den.Insert(ctx, db, bad))
	require.NoError(t, den.Insert(ctx, db, last))

	var errs []error
	var names []string
	for d, err := range den.NewQuery[IterLinkedDoc](db).Sort("_id", den.Asc).WithFetchLinks().Iter(ctx) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		names = append(names, d.Name)
	}

	require.Len(t, errs, 1, "exactly one error (the dangling link) — iterator must terminate, not continue")
	require.ErrorIs(t, errs[0], den.ErrNotFound)
	assert.Equal(t, []string{"A"}, names, "row after the error must not be yielded")
}

func TestIter_WithConditions(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*Product{
		{Name: "A", Price: 10.0},
		{Name: "B", Price: 20.0},
		{Name: "C", Price: 30.0},
	}))

	var names []string
	for p, err := range den.NewQuery[Product](db, where.Field("price").Gt(15.0)).Iter(ctx) {
		require.NoError(t, err)
		names = append(names, p.Name)
	}
	assert.Len(t, names, 2)
}
