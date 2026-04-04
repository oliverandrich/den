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

type TrackedProduct struct {
	document.TrackedBase
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

func TestIsChanged_NoChanges(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	found, err := den.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	changed, err := den.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestIsChanged_WithChanges(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	found, err := den.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	found.Price = 99.0

	changed, err := den.IsChanged(db, found)
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestIsChanged_NoSnapshot(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})

	// Doc created locally, never loaded from DB
	p := &TrackedProduct{Name: "New"}

	changed, err := den.IsChanged(db, p)
	require.NoError(t, err)
	assert.False(t, changed, "no snapshot means not changed")
}

func TestGetChanges(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	found, err := den.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	found.Price = 99.0
	found.Name = "Updated"

	changes, err := den.GetChanges(db, found)
	require.NoError(t, err)
	assert.Contains(t, changes, "price")
	assert.Contains(t, changes, "name")
	assert.InDelta(t, 10.0, changes["price"].Before, 0.001)
	assert.InDelta(t, 99.0, changes["price"].After, 0.001)
}

func TestGetChanges_NoSnapshot(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})

	p := &TrackedProduct{Name: "New"}

	changes, err := den.GetChanges(db, p)
	require.NoError(t, err)
	assert.Nil(t, changes)
}

func TestGetChanges_NoChanges(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	found, err := den.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	changes, err := den.GetChanges(db, found)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestGetChanges_FieldRemoved(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	found, err := den.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	// Set name to empty — appears as change
	found.Name = ""

	changes, err := den.GetChanges(db, found)
	require.NoError(t, err)
	assert.Contains(t, changes, "name")
}

func TestUpdate_RefreshesSnapshot(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	found, err := den.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	found.Price = 20.0
	require.NoError(t, den.Update(ctx, db, found))

	// After Update, snapshot should be refreshed
	changed, err := den.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed, "should not be changed after Update")
}

func TestRollback(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	found, err := den.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	found.Price = 99.0
	found.Name = "Changed"

	require.NoError(t, den.Rollback(db, found))
	assert.Equal(t, "Widget", found.Name)
	assert.InDelta(t, 10.0, found.Price, 0.001)

	// After rollback, should not be changed
	changed, err := den.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestRollback_NoSnapshot(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})

	p := &TrackedProduct{Name: "New"}

	err := den.Rollback(db, p)
	require.ErrorIs(t, err, den.ErrNoSnapshot)
}

func TestIsChanged_ViaIter(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	for doc, err := range den.NewQuery[TrackedProduct](ctx, db).Iter() {
		require.NoError(t, err)
		doc.Price = 42.0
		changed, err := den.IsChanged(db, doc)
		require.NoError(t, err)
		assert.True(t, changed)
	}
}

type TrackedSoftProduct struct {
	document.TrackedSoftBase
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

func TestTrackedSoftBase_TrackingAndSoftDelete(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedSoftProduct{})
	ctx := context.Background()

	p := &TrackedSoftProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	found, err := den.FindByID[TrackedSoftProduct](ctx, db, p.ID)
	require.NoError(t, err)

	// Tracking works
	found.Price = 99.0
	changed, err := den.IsChanged(db, found)
	require.NoError(t, err)
	assert.True(t, changed)

	// Soft-delete works
	require.NoError(t, den.Delete(ctx, db, found))
	assert.True(t, found.IsDeleted())

	// Hidden from normal queries
	results, err := den.NewQuery[TrackedSoftProduct](ctx, db).All()
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestIsChanged_UntrackedType(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	found, err := den.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)

	// Product uses Base, not TrackedBase — always reports no changes
	changed, err := den.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed)
}
