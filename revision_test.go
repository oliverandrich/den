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

// TestRevision_SoftDeleteRestoreUpdate verifies that soft-delete participates
// in the revision chain: Rev bumps on soft-delete so a concurrent writer
// holding the pre-delete revision cannot silently clobber DeletedAt.
func TestRevision_SoftDeleteRestoreUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &SoftRevProduct{})
	ctx := context.Background()

	p := &SoftRevProduct{Name: "v1"}
	require.NoError(t, den.Insert(ctx, db, p))
	revInsert := p.Rev
	require.NotEmpty(t, revInsert)

	require.NoError(t, den.Delete(ctx, db, p)) // soft-delete
	require.True(t, p.IsDeleted())
	revDelete := p.Rev
	assert.NotEqual(t, revInsert, revDelete,
		"soft-delete must bump Rev so concurrent Updates detect the conflict")

	p.DeletedAt = nil // restore
	p.Name = "restored"
	require.NoError(t, den.Update(ctx, db, p))
	revRestore := p.Rev
	assert.NotEqual(t, revDelete, revRestore,
		"restore goes through Update and must bump Rev again")

	p.Name = "v2"
	require.NoError(t, den.Update(ctx, db, p))
	assert.NotEqual(t, revRestore, p.Rev,
		"subsequent Update must continue to bump Rev after restore")
}

// TestRevision_ConcurrentSoftDeleteUpdate pins the fix for den-mloq: a
// concurrent writer holding the pre-delete revision must see
// ErrRevisionConflict on Update after another goroutine soft-deletes the
// document. Before the fix, soft-delete bypassed checkAndUpdateRevision and
// the stale Update silently clobbered DeletedAt.
func TestRevision_ConcurrentSoftDeleteUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &SoftRevProduct{})
	ctx := context.Background()

	p := &SoftRevProduct{Name: "v1"}
	require.NoError(t, den.Insert(ctx, db, p))

	// Two independent loads, identical starting rev.
	a, err := den.FindByID[SoftRevProduct](ctx, db, p.ID)
	require.NoError(t, err)
	b, err := den.FindByID[SoftRevProduct](ctx, db, p.ID)
	require.NoError(t, err)
	require.Equal(t, a.Rev, b.Rev)

	// Soft-delete via a; b still holds the pre-delete revision.
	require.NoError(t, den.Delete(ctx, db, a))

	b.Name = "would-clobber-deletion"
	err = den.Update(ctx, db, b)
	require.ErrorIs(t, err, den.ErrRevisionConflict,
		"stale Update after concurrent soft-delete must conflict, not overwrite DeletedAt")

	// Stored state must still show the soft-deletion.
	found, err := den.FindByID[SoftRevProduct](ctx, db, p.ID)
	require.NoError(t, err)
	assert.True(t, found.IsDeleted(), "DeletedAt must survive the stale Update attempt")
	assert.Equal(t, "v1", found.Name, "stale Update must not take effect")
}

// SoftRevHouse links to a revisioned soft-delete-capable target so cascade
// soft-delete can be exercised against the revision chain.
type SoftRevHouse struct {
	document.Base
	Name string                   `json:"name"`
	Door den.Link[SoftRevProduct] `json:"door"`
}

// TestRevision_CascadeSoftDeleteBumpsLinkedRevision pins that the
// LinkDelete cascade soft-deletes its targets through the revision chain
// — a concurrent writer holding the pre-cascade revision must see
// ErrRevisionConflict on Update, not silently clobber the cascade-set
// DeletedAt. Before this fix the cascade did a raw Put, leaving the
// stored _rev unchanged and inviting the lost-update race.
func TestRevision_CascadeSoftDeleteBumpsLinkedRevision(t *testing.T) {
	db := dentest.MustOpen(t, &SoftRevProduct{}, &SoftRevHouse{})
	ctx := context.Background()

	door := &SoftRevProduct{Name: "v1"}
	require.NoError(t, den.Insert(ctx, db, door))

	house := &SoftRevHouse{Name: "Home", Door: den.NewLink(door)}
	require.NoError(t, den.Insert(ctx, db, house))

	// Snapshot the door as a concurrent writer would have it.
	stale := *door

	// Cascade soft-delete via the parent.
	require.NoError(t, den.Delete(ctx, db, house, den.WithLinkRule(den.LinkDelete)))

	// The concurrent writer's stale-rev Update must conflict.
	stale.Name = "would-clobber-deletion"
	err := den.Update(ctx, db, &stale)
	require.ErrorIs(t, err, den.ErrRevisionConflict,
		"cascade soft-delete must bump linked _rev so concurrent stale Update conflicts")

	// Stored doc still soft-deleted, name unchanged.
	found, err := den.NewQuery[SoftRevProduct](db).IncludeDeleted().First(ctx)
	require.NoError(t, err)
	assert.True(t, found.IsDeleted(), "DeletedAt must survive the stale Update attempt")
	assert.Equal(t, "v1", found.Name, "stale Update must not take effect")
}

// TestRevision_SoftDeleteIgnoreRevision confirms that HardDelete-less
// soft-deletes still respect IgnoreRevision — callers that deliberately want
// to bypass the check can do so.
func TestRevision_SoftDeleteIgnoreRevision(t *testing.T) {
	db := dentest.MustOpen(t, &SoftRevProduct{})
	ctx := context.Background()

	p := &SoftRevProduct{Name: "v1"}
	require.NoError(t, den.Insert(ctx, db, p))

	// Mutate the in-memory rev so the check would fail without IgnoreRevision.
	p.Rev = "stale"
	require.NoError(t, den.Delete(ctx, db, p, den.IgnoreRevision()))
	assert.True(t, p.IsDeleted())
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
