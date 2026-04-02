package den_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
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
	for p, err := range den.NewQuery[Product](ctx, db).Iter() {
		require.NoError(t, err)
		names = append(names, p.Name)
	}
	assert.Len(t, names, 3)
}

func TestIter_Empty(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	count := 0
	for _, err := range den.NewQuery[Product](ctx, db).Iter() {
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
	for _, err := range den.NewQuery[Product](ctx, db).Iter() {
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
	for p, err := range den.NewQuery[SoftProduct](ctx, db).Iter() {
		require.NoError(t, err)
		names = append(names, p.Name)
	}
	assert.Equal(t, []string{"Keep"}, names)
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
	for p, err := range den.NewQuery[Product](ctx, db, where.Field("price").Gt(15.0)).Iter() {
		require.NoError(t, err)
		names = append(names, p.Name)
	}
	assert.Len(t, names, 2)
}
