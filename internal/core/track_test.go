package core_test

import (
	"github.com/oliverandrich/den/internal/core"

	"context"
	"testing"

	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TrackedProduct struct {
	document.Base
	document.Tracked
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

func TestIsChanged_NoChanges(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))

	found, err := core.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	changed, err := core.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestIsChanged_WithChanges(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))

	found, err := core.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	found.Price = 99.0

	changed, err := core.IsChanged(db, found)
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestIsChanged_NoSnapshot(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})

	// Doc created locally, never loaded from DB
	p := &TrackedProduct{Name: "New"}

	changed, err := core.IsChanged(db, p)
	require.NoError(t, err)
	assert.False(t, changed, "no snapshot means not changed")
}

func TestGetChanges(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))

	found, err := core.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	found.Price = 99.0
	found.Name = "Updated"

	changes, err := core.GetChanges(db, found)
	require.NoError(t, err)
	assert.Contains(t, changes, "price")
	assert.Contains(t, changes, "name")
	assert.InDelta(t, 10.0, changes["price"].Before, 0.001)
	assert.InDelta(t, 99.0, changes["price"].After, 0.001)
}

func TestGetChanges_NoSnapshot(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})

	p := &TrackedProduct{Name: "New"}

	changes, err := core.GetChanges(db, p)
	require.NoError(t, err)
	assert.Nil(t, changes)
}

func TestGetChanges_NoChanges(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))

	found, err := core.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	changes, err := core.GetChanges(db, found)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestGetChanges_FieldRemoved(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))

	found, err := core.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	// Set name to empty — appears as change
	found.Name = ""

	changes, err := core.GetChanges(db, found)
	require.NoError(t, err)
	assert.Contains(t, changes, "name")
}

func TestUpdate_RefreshesSnapshot(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))

	found, err := core.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	found.Price = 20.0
	require.NoError(t, core.Save(ctx, db, found))

	// After Update, snapshot should be refreshed
	changed, err := core.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed, "should not be changed after Update")
}

func TestRevert(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))

	found, err := core.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	found.Price = 99.0
	found.Name = "Changed"

	require.NoError(t, core.Revert(db, found))
	assert.Equal(t, "Widget", found.Name)
	assert.InDelta(t, 10.0, found.Price, 0.001)

	// After revert, should not be changed
	changed, err := core.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestRevert_NoSnapshot(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})

	p := &TrackedProduct{Name: "New"}

	err := core.Revert(db, p)
	require.ErrorIs(t, err, core.ErrNoSnapshot)
}

func TestIsChanged_ViaIter(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))

	for doc, err := range core.NewQuery[TrackedProduct](db).Iter(ctx) {
		require.NoError(t, err)
		doc.Price = 42.0
		changed, err := core.IsChanged(db, doc)
		require.NoError(t, err)
		assert.True(t, changed)
	}
}

type TrackedSoftProduct struct {
	document.Base
	document.SoftDelete
	document.Tracked
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

func TestTrackedSoftDelete_Composition(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedSoftProduct{})
	ctx := context.Background()

	p := &TrackedSoftProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))

	found, err := core.FindByID[TrackedSoftProduct](ctx, db, p.ID)
	require.NoError(t, err)

	// Tracking works
	found.Price = 99.0
	changed, err := core.IsChanged(db, found)
	require.NoError(t, err)
	assert.True(t, changed)

	// Soft-delete works
	require.NoError(t, core.Delete(ctx, db, found))
	assert.True(t, found.IsDeleted())

	// Hidden from normal queries
	results, err := core.NewQuery[TrackedSoftProduct](db).All(ctx)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestIsChanged_UntrackedType(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))

	found, err := core.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)

	// Product uses Base, not TrackedBase — always reports no changes
	changed, err := core.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed)
}
