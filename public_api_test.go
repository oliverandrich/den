// public_api_test.go exercises the root API skin (wrappers in den.go,
// crud.go, query.go, options.go) at integration level. The wrappers are
// one-line delegates to den/engine; the engine logic is tested against
// `engine.X` in engine's own test suite. The tests here pin that the
// wrappers route through correctly — without them, the root package
// shows zero coverage because no other test file imports it.

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

type smokeAuthor struct {
	document.Base
	document.Tracked
	Name string `json:"name" den:"index"`
}

type smokeBook struct {
	document.Base
	document.SoftDelete
	Title  string                `json:"title" den:"index"`
	Author den.Link[smokeAuthor] `json:"author"`
}

// TestPublicAPI_DocLifecycle walks a doc through save → query → refresh
// → tracked-changes → revert → delete using the root wrappers. Covers:
// Save, SaveAll, FindByID, FindByIDs, NewQuery, Refresh, RefreshAll,
// IsChanged, GetChanges, Revert, Delete, DeleteAll, Meta, NewID.
func TestPublicAPI_DocLifecycle(t *testing.T) {
	db := dentest.MustOpen(t, &smokeAuthor{}, &smokeBook{})
	ctx := context.Background()

	// NewID — public ULID generator.
	id := den.NewID()
	assert.Len(t, id, 26)

	// Meta — registered metadata round-trip.
	meta, err := den.Meta[smokeAuthor](db)
	require.NoError(t, err)
	assert.NotEmpty(t, meta.Name)

	// Save (insert branch).
	a := &smokeAuthor{Name: "Ada Lovelace"}
	require.NoError(t, den.Save(ctx, db, a))
	require.NotEmpty(t, a.ID)

	// Save (update branch) — Tracked snapshot captured on Save.
	a.Name = "Ada King"
	require.NoError(t, den.Save(ctx, db, a))

	// IsChanged / GetChanges / Revert on a Tracked doc.
	a.Name = "drafty edit"
	changed, err := den.IsChanged(db, a)
	require.NoError(t, err)
	require.True(t, changed)

	changes, err := den.GetChanges(db, a)
	require.NoError(t, err)
	require.Contains(t, changes, "name")

	require.NoError(t, den.Revert(db, a))
	assert.Equal(t, "Ada King", a.Name)

	// SaveAll batch.
	more := []*smokeAuthor{
		{Name: "Grace Hopper"},
		{Name: "Margaret Hamilton"},
	}
	require.NoError(t, den.SaveAll(ctx, db, more))
	for _, m := range more {
		require.NotEmpty(t, m.ID)
	}

	// FindByID / FindByIDs.
	got, err := den.FindByID[smokeAuthor](ctx, db, a.ID)
	require.NoError(t, err)
	assert.Equal(t, "Ada King", got.Name)

	ids := []string{a.ID, more[0].ID, more[1].ID}
	gotMany, err := den.FindByIDs[smokeAuthor](ctx, db, ids)
	require.NoError(t, err)
	assert.Len(t, gotMany, 3)

	// NewQuery — the entry point to the QuerySet builder.
	all, err := den.NewQuery[smokeAuthor](db).Sort("name", den.Asc).All(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// Refresh — re-read state.
	require.NoError(t, den.Refresh(ctx, db, a))

	// RefreshAll — batch re-read.
	require.NoError(t, den.RefreshAll(ctx, db, more))

	// Delete (soft on a SoftDelete doc happens with smokeBook; here delete the author).
	require.NoError(t, den.Delete(ctx, db, a))

	// DeleteAll batch.
	require.NoError(t, den.DeleteAll(ctx, db, more))
}

// TestPublicAPI_Links covers NewLink, FetchLink, FetchLinkField,
// FetchAllLinks via the root wrappers.
func TestPublicAPI_Links(t *testing.T) {
	db := dentest.MustOpen(t, &smokeAuthor{}, &smokeBook{})
	ctx := context.Background()

	author := &smokeAuthor{Name: "Author"}
	require.NoError(t, den.Save(ctx, db, author))

	// NewLink — extract an ID into a typed Link.
	book := &smokeBook{Title: "Title", Author: den.NewLink(author)}
	require.NoError(t, den.Save(ctx, db, book))

	// FetchLink — resolve by JSON field name.
	loaded, err := den.FindByID[smokeBook](ctx, db, book.ID)
	require.NoError(t, err)
	require.NoError(t, den.FetchLink(ctx, db, loaded, "author"))
	require.True(t, loaded.Author.IsLoaded())
	require.NotNil(t, loaded.Author.Value)

	// FetchLinkField — resolve by typed pointer.
	loaded2, err := den.FindByID[smokeBook](ctx, db, book.ID)
	require.NoError(t, err)
	require.NoError(t, den.FetchLinkField(ctx, db, &loaded2.Author))
	require.True(t, loaded2.Author.IsLoaded())

	// FetchAllLinks — single-level hydration of every link.
	loaded3, err := den.FindByID[smokeBook](ctx, db, book.ID)
	require.NoError(t, err)
	require.NoError(t, den.FetchAllLinks(ctx, db, loaded3))
	require.True(t, loaded3.Author.IsLoaded())
}

// TestPublicAPI_Transactions covers RunInTransaction, LockByID,
// AdvisoryLock via the root wrappers. SQLite's locking is coarse-grained
// but the wrappers still route through.
func TestPublicAPI_Transactions(t *testing.T) {
	db := dentest.MustOpen(t, &smokeAuthor{})
	ctx := context.Background()

	a := &smokeAuthor{Name: "Tx target"}
	require.NoError(t, den.Save(ctx, db, a))

	require.NoError(t, den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		// LockByID — SQLite no-ops on row locks, but the wrapper still routes.
		locked, err := den.LockByID[smokeAuthor](ctx, tx, a.ID)
		if err != nil {
			return err
		}
		if locked.Name != "Tx target" {
			t.Errorf("locked doc has unexpected name: %q", locked.Name)
		}

		// AdvisoryLock — SQLite no-ops; the wrapper still routes.
		return den.AdvisoryLock(ctx, tx, 42)
	}))
}

// TestPublicAPI_Options exercises every CRUDOption / LockOption / Option
// constructor. The wrappers return functional options; calling them is
// enough to cover the route.
func TestPublicAPI_Options(t *testing.T) {
	// CRUDOptions — exercised in real calls below where it matters; here
	// we just prove the constructors return non-nil.
	assert.NotNil(t, den.WithLinkRule(den.LinkWrite))
	assert.NotNil(t, den.WithoutFetchLinks())
	assert.NotNil(t, den.HardDelete())
	assert.NotNil(t, den.IncludeDeleted())
	assert.NotNil(t, den.SoftDeleteBy("usr-1"))
	assert.NotNil(t, den.SoftDeleteReason("cleanup"))
	assert.NotNil(t, den.IgnoreRevision())

	// LockOptions.
	assert.NotNil(t, den.NoWait())
	assert.NotNil(t, den.SkipLocked())

	// Open options.
	assert.NotNil(t, den.WithTypes(&smokeAuthor{}))
	assert.NotNil(t, den.WithStorage(nil)) // nil Storage is valid syntactically; never opens.

	// DropStaleOption.
	assert.NotNil(t, den.DryRun())

	// Now drive a few through real calls so the option-application path
	// also gets coverage.
	db := dentest.MustOpen(t, &smokeBook{})
	ctx := context.Background()

	b := &smokeBook{Title: "Option doc"}
	require.NoError(t, den.Save(ctx, db, b))

	// HardDelete bypasses the soft-delete path on a SoftDelete doc.
	require.NoError(t, den.Delete(ctx, db, b, den.HardDelete()))

	// FindByID with IncludeDeleted opt — already auto-applied for FindByID
	// in v0.12; this confirms the option round-trips.
	_, err := den.FindByID[smokeBook](ctx, db, b.ID, den.IncludeDeleted(), den.WithoutFetchLinks())
	require.ErrorIs(t, err, den.ErrNotFound) // hard-deleted, gone for good.
}

// TestPublicAPI_Open covers the `Open` wrapper (the manual-backend
// constructor) and DropStaleIndexes via the root wrappers. OpenURL is
// already exercised by every other test via dentest.
func TestPublicAPI_Open(t *testing.T) {
	db := dentest.MustOpen(t, &smokeAuthor{})
	ctx := context.Background()

	// Open with a Backend obtained from an existing DB.
	manual, err := den.Open(ctx, db.Backend())
	require.NoError(t, err)
	require.NotNil(t, manual)

	// DropStaleIndexes — dry-run so no schema change actually fires.
	res, err := den.DropStaleIndexes(ctx, db, den.DryRun())
	require.NoError(t, err)
	_ = res // result shape covered by engine tests.
}
