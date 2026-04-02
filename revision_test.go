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

type RevProduct struct {
	document.Base
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

func (p RevProduct) DenSettings() den.Settings {
	return den.Settings{UseRevision: true}
}

func TestRevision_SetOnInsert(t *testing.T) {
	db := dentest.MustOpen(t, &RevProduct{})
	ctx := context.Background()

	p := &RevProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))
	assert.NotEmpty(t, p.Rev, "revision should be set on insert")
}

func TestRevision_UpdatesOnUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &RevProduct{})
	ctx := context.Background()

	p := &RevProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))
	firstRev := p.Rev

	p.Price = 20.0
	require.NoError(t, den.Update(ctx, db, p))
	assert.NotEqual(t, firstRev, p.Rev, "revision should change on update")
}

func TestRevision_Conflict(t *testing.T) {
	db := dentest.MustOpen(t, &RevProduct{})
	ctx := context.Background()

	p := &RevProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	// Simulate a concurrent update
	p2, err := den.FindByID[RevProduct](ctx, db, p.ID)
	require.NoError(t, err)
	p2.Price = 30.0
	require.NoError(t, den.Update(ctx, db, p2))

	// p still has the old revision — should conflict
	p.Price = 99.0
	err = den.Update(ctx, db, p)
	require.ErrorIs(t, err, den.ErrRevisionConflict)
}

func TestRevision_IgnoreRevision(t *testing.T) {
	db := dentest.MustOpen(t, &RevProduct{})
	ctx := context.Background()

	p := &RevProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	// Simulate stale revision
	p2, _ := den.FindByID[RevProduct](ctx, db, p.ID)
	p2.Price = 30.0
	require.NoError(t, den.Update(ctx, db, p2))

	// Force write with stale revision
	p.Price = 99.0
	require.NoError(t, den.Insert(ctx, db, p, den.IgnoreRevision()))

	found, err := den.FindByID[RevProduct](ctx, db, p.ID)
	require.NoError(t, err)
	assert.InDelta(t, 99.0, found.Price, 0.001)
}
