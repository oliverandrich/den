package den_test

import (
	"context"
	"errors"
	"sync"
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

// SoftRevProduct combines revision tracking with soft-delete to exercise the
// interaction between the two features.
type SoftRevProduct struct {
	document.Base
	document.SoftDelete
	Name string `json:"name"`
}

func (p SoftRevProduct) DenSettings() den.Settings {
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

// TestRevision_BlindUpdateConflict reproduces den-b0mq: a caller constructs
// a fresh doc with only the ID (no _rev) and calls Update. Without the fix,
// the revision check is silently skipped (empty currentRev short-circuits the
// guard) and the write clobbers the stored document. Expected behavior: the
// empty in-memory rev must not match the populated DB rev → ErrRevisionConflict.
func TestRevision_BlindUpdateConflict(t *testing.T) {
	db := dentest.MustOpen(t, &RevProduct{})
	ctx := context.Background()

	original := &RevProduct{Name: "v1", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, original))

	blind := &RevProduct{Name: "blind overwrite", Price: 999.0}
	blind.ID = original.ID
	// blind.Rev is the zero value — intentionally not set

	err := den.Update(ctx, db, blind)
	require.ErrorIs(t, err, den.ErrRevisionConflict,
		"update of a revisioned doc with an empty _rev must conflict, not silently overwrite")

	// Confirm the write did not go through.
	found, err := den.FindByID[RevProduct](ctx, db, original.ID)
	require.NoError(t, err)
	assert.Equal(t, "v1", found.Name, "original document must be untouched")
}

// TestRevision_EmptyRevAfterLoad_Conflicts covers the complement of
// TestRevision_BlindUpdateConflict: a caller loaded the document via FindByID
// (so Rev is populated) but then manually cleared Rev before calling Update.
// Same semantics should apply — empty in-memory Rev against a populated stored
// Rev is always a conflict.
func TestRevision_EmptyRevAfterLoad_Conflicts(t *testing.T) {
	db := dentest.MustOpen(t, &RevProduct{})
	ctx := context.Background()

	original := &RevProduct{Name: "v1", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, original))

	loaded, err := den.FindByID[RevProduct](ctx, db, original.ID)
	require.NoError(t, err)
	loaded.Rev = "" // manually clear after load
	loaded.Price = 99.0

	err = den.Update(ctx, db, loaded)
	require.ErrorIs(t, err, den.ErrRevisionConflict,
		"clearing Rev after load must produce the same conflict as a fresh blind-update doc")
}

// TestRevision_ConcurrentUpdates_ExactlyOneSucceeds drives two goroutines to
// load the same revision then race their Updates. SQLite's IMMEDIATE tx
// serializes the writes; whichever lands second reads a bumped _rev from the
// store and fails with ErrRevisionConflict.
func TestRevision_ConcurrentUpdates_ExactlyOneSucceeds(t *testing.T) {
	db := dentest.MustOpen(t, &RevProduct{})
	ctx := context.Background()

	original := &RevProduct{Name: "shared", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, original))

	// Pre-load the same revision into N independent doc instances in the main
	// goroutine, so every racer starts with the identical stale rev. Loading
	// inside the goroutines allowed the second goroutine to read a post-commit
	// rev from the first one's Update, which defeats the race entirely.
	const N = 2
	docs := make([]*RevProduct, N)
	for i := range N {
		loaded, err := den.FindByID[RevProduct](ctx, db, original.ID)
		require.NoError(t, err)
		require.Equal(t, original.Rev, loaded.Rev, "pre-load must see the insert rev")
		docs[i] = loaded
	}

	start := make(chan struct{})
	errs := make(chan error, N)
	var wg sync.WaitGroup
	for i := range N {
		wg.Go(func() {
			<-start // barrier: release both goroutines simultaneously
			docs[i].Price = float64(100 + i)
			errs <- den.Update(ctx, db, docs[i])
		})
	}
	close(start)
	wg.Wait()
	close(errs)

	var success, conflicts int
	for err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, den.ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	assert.Equal(t, 1, success, "exactly one goroutine must win")
	assert.Equal(t, 1, conflicts, "the other must see ErrRevisionConflict")
}

// TestRevision_SoftDeleteRestoreUpdate pins the current (surprising) behavior
// that a soft-delete bypasses the revision chain — Rev stays the same across
// Delete. A subsequent Update that restores the document (clears DeletedAt)
// goes through the regular Update path and does bump Rev. See den-mloq for
// the concurrent-clobber discussion this enables.
func TestRevision_SoftDeleteRestoreUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &SoftRevProduct{})
	ctx := context.Background()

	p := &SoftRevProduct{Name: "v1"}
	require.NoError(t, den.Insert(ctx, db, p))
	revInsert := p.Rev
	require.NotEmpty(t, revInsert)

	require.NoError(t, den.Delete(ctx, db, p)) // soft-delete
	require.True(t, p.IsDeleted())
	assert.Equal(t, revInsert, p.Rev,
		"current behavior: soft-delete bypasses the revision chain (see den-mloq)")

	p.DeletedAt = nil // restore
	p.Name = "restored"
	require.NoError(t, den.Update(ctx, db, p))
	revRestore := p.Rev
	assert.NotEqual(t, revInsert, revRestore,
		"restore goes through Update and must bump Rev")

	p.Name = "v2"
	require.NoError(t, den.Update(ctx, db, p))
	assert.NotEqual(t, revRestore, p.Rev,
		"subsequent Update must continue to bump Rev after restore")
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
