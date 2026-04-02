package den_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
)

type SoftProduct struct {
	document.SoftBase
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

func TestSoftDelete(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p := &SoftProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	require.NoError(t, den.Delete(ctx, db, p))
	assert.True(t, p.IsDeleted())

	// Should be hidden from normal queries
	results, err := den.NewQuery[SoftProduct](ctx, db).All()
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestSoftDelete_IncludeDeleted(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p := &SoftProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))
	require.NoError(t, den.Delete(ctx, db, p))

	// IncludeDeleted shows soft-deleted docs
	results, err := den.NewQuery[SoftProduct](ctx, db).IncludeDeleted().All()
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.True(t, results[0].IsDeleted())
}

func TestSoftDelete_FindByID_StillAccessible(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p := &SoftProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))
	require.NoError(t, den.Delete(ctx, db, p))

	// FindByID still returns the document (it's a direct key lookup)
	found, err := den.FindByID[SoftProduct](ctx, db, p.ID)
	require.NoError(t, err)
	assert.True(t, found.IsDeleted())
}

func TestHardDelete(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p := &SoftProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))
	require.NoError(t, den.Delete(ctx, db, p))

	// Hard delete permanently removes
	require.NoError(t, den.HardDelete(ctx, db, p))

	_, err := den.FindByID[SoftProduct](ctx, db, p.ID)
	require.ErrorIs(t, err, den.ErrNotFound)
}

func TestSoftDelete_Count(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p1 := &SoftProduct{Name: "Keep", Price: 10.0}
	p2 := &SoftProduct{Name: "Delete", Price: 20.0}
	require.NoError(t, den.Insert(ctx, db, p1))
	require.NoError(t, den.Insert(ctx, db, p2))
	require.NoError(t, den.Delete(ctx, db, p2))

	count, err := den.NewQuery[SoftProduct](ctx, db).Count()
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}
