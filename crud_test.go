// Smoke tests for the wrappers in crud.go (Save, Delete, FindByID,
// Refresh, Tracked-changes helpers, Link helpers). See den_test.go for
// shared fixture types and the rationale for these tests.

package den_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
)

// TestCRUD_DocLifecycle walks a doc through Save / SaveAll / FindByID /
// FindByIDs / Refresh / RefreshAll / Delete / DeleteAll.
func TestCRUD_DocLifecycle(t *testing.T) {
	db := dentest.MustOpen(t, &smokeAuthor{})
	ctx := context.Background()

	// Save (insert branch).
	a := &smokeAuthor{Name: "Ada Lovelace"}
	require.NoError(t, den.Save(ctx, db, a))
	require.NotEmpty(t, a.ID)

	// Save (update branch).
	a.Name = "Ada King"
	require.NoError(t, den.Save(ctx, db, a))

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

	// Refresh / RefreshAll.
	require.NoError(t, den.Refresh(ctx, db, a))
	require.NoError(t, den.RefreshAll(ctx, db, more))

	// Delete / DeleteAll.
	require.NoError(t, den.Delete(ctx, db, a))
	require.NoError(t, den.DeleteAll(ctx, db, more))
}

// TestCRUD_Tracking covers IsChanged / GetChanges / Revert on a Tracked
// doc. Two Saves before mutate-and-revert pin that the snapshot refreshes
// on the second Save — otherwise Revert would restore to the first Save's
// value and the test would silently miss a regression in the
// snapshot-update path.
func TestCRUD_Tracking(t *testing.T) {
	db := dentest.MustOpen(t, &smokeAuthor{})
	ctx := context.Background()

	a := &smokeAuthor{Name: "Ada Lovelace"}
	require.NoError(t, den.Save(ctx, db, a))

	// Second Save must refresh the Tracked snapshot to the new value.
	a.Name = "Ada King"
	require.NoError(t, den.Save(ctx, db, a))

	a.Name = "drafty edit"
	changed, err := den.IsChanged(db, a)
	require.NoError(t, err)
	require.True(t, changed)

	changes, err := den.GetChanges(db, a)
	require.NoError(t, err)
	require.Contains(t, changes, "name")

	require.NoError(t, den.Revert(db, a))
	assert.Equal(t, "Ada King", a.Name, "Revert restores the most recent snapshot, not the original")
}

// TestCRUD_Links covers NewLink, FetchLink, FetchLinkField, FetchAllLinks.
func TestCRUD_Links(t *testing.T) {
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
