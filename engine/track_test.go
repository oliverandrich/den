package engine_test

import (
	"github.com/oliverandrich/den/engine"

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
	require.NoError(t, engine.Save(ctx, db, p))

	found, err := engine.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	changed, err := engine.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestIsChanged_WithChanges(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, engine.Save(ctx, db, p))

	found, err := engine.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	found.Price = 99.0

	changed, err := engine.IsChanged(db, found)
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestIsChanged_NoSnapshot(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})

	// Doc created locally, never loaded from DB
	p := &TrackedProduct{Name: "New"}

	changed, err := engine.IsChanged(db, p)
	require.NoError(t, err)
	assert.False(t, changed, "no snapshot means not changed")
}

func TestGetChanges(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, engine.Save(ctx, db, p))

	found, err := engine.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	found.Price = 99.0
	found.Name = "Updated"

	changes, err := engine.GetChanges(db, found)
	require.NoError(t, err)
	assert.Contains(t, changes, "price")
	assert.Contains(t, changes, "name")
	assert.InDelta(t, 10.0, changes["price"].Before, 0.001)
	assert.InDelta(t, 99.0, changes["price"].After, 0.001)
}

func TestGetChanges_NoSnapshot(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})

	p := &TrackedProduct{Name: "New"}

	changes, err := engine.GetChanges(db, p)
	require.NoError(t, err)
	assert.Nil(t, changes)
}

func TestGetChanges_NoChanges(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, engine.Save(ctx, db, p))

	found, err := engine.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	changes, err := engine.GetChanges(db, found)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestGetChanges_FieldRemoved(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, engine.Save(ctx, db, p))

	found, err := engine.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	// Set name to empty — appears as change
	found.Name = ""

	changes, err := engine.GetChanges(db, found)
	require.NoError(t, err)
	assert.Contains(t, changes, "name")
}

func TestUpdate_RefreshesSnapshot(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, engine.Save(ctx, db, p))

	found, err := engine.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	found.Price = 20.0
	require.NoError(t, engine.Save(ctx, db, found))

	// After Update, snapshot should be refreshed
	changed, err := engine.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed, "should not be changed after Update")
}

func TestRevert(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, engine.Save(ctx, db, p))

	found, err := engine.FindByID[TrackedProduct](ctx, db, p.ID)
	require.NoError(t, err)

	found.Price = 99.0
	found.Name = "Changed"

	require.NoError(t, engine.Revert(db, found))
	assert.Equal(t, "Widget", found.Name)
	assert.InDelta(t, 10.0, found.Price, 0.001)

	// After revert, should not be changed
	changed, err := engine.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestRevert_NoSnapshot(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})

	p := &TrackedProduct{Name: "New"}

	err := engine.Revert(db, p)
	require.ErrorIs(t, err, engine.ErrNoSnapshot)
}

func TestIsChanged_ViaIter(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedProduct{})
	ctx := context.Background()

	p := &TrackedProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, engine.Save(ctx, db, p))

	for doc, err := range engine.NewQuery[TrackedProduct](db).Iter(ctx) {
		require.NoError(t, err)
		doc.Price = 42.0
		changed, err := engine.IsChanged(db, doc)
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
	require.NoError(t, engine.Save(ctx, db, p))

	found, err := engine.FindByID[TrackedSoftProduct](ctx, db, p.ID)
	require.NoError(t, err)

	// Tracking works
	found.Price = 99.0
	changed, err := engine.IsChanged(db, found)
	require.NoError(t, err)
	assert.True(t, changed)

	// Soft-delete works
	require.NoError(t, engine.Delete(ctx, db, found))
	assert.True(t, found.IsDeleted())

	// Hidden from normal queries
	results, err := engine.NewQuery[TrackedSoftProduct](db).All(ctx)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestIsChanged_UntrackedType(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 10.0}
	require.NoError(t, engine.Save(ctx, db, p))

	found, err := engine.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)

	// Product uses Base, not TrackedBase — always reports no changes
	changed, err := engine.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed)
}

// Change tracking on nested struct fields (den-1351 walker support).
// The diff is computed at the top-level JSON-key granularity, so a
// change inside a nested struct surfaces under the outer field's name
// (`profile` → `FieldChange{Before: {…}, After: {…}}`), not as a
// dotted path. This is intentional — atomic sub-document updates stay
// visible as a single logical change. Tests pin both directions
// (IsChanged + Revert) and the granularity contract for GetChanges.

type trackedProfile struct {
	Slug string `json:"slug"`
	Bio  string `json:"bio"`
}

type TrackedNestedDoc struct {
	document.Base
	document.Tracked
	Email   string         `json:"email"`
	Profile trackedProfile `json:"profile"`
}

type TrackedNestedPtrDoc struct {
	document.Base
	document.Tracked
	Email   string          `json:"email"`
	Profile *trackedProfile `json:"profile,omitempty"`
}

func TestIsChanged_NestedValueField(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedNestedDoc{})
	ctx := context.Background()

	doc := &TrackedNestedDoc{Email: "a@example.com", Profile: trackedProfile{Slug: "ada", Bio: "old"}}
	require.NoError(t, engine.Save(ctx, db, doc))

	found, err := engine.FindByID[TrackedNestedDoc](ctx, db, doc.ID)
	require.NoError(t, err)

	changed, err := engine.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed, "freshly loaded doc reports no changes")

	found.Profile.Bio = "new"
	changed, err = engine.IsChanged(db, found)
	require.NoError(t, err)
	assert.True(t, changed, "modifying a nested-value field must surface as changed")
}

func TestGetChanges_NestedValueFieldGranularity(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedNestedDoc{})
	ctx := context.Background()

	doc := &TrackedNestedDoc{Email: "a@example.com", Profile: trackedProfile{Slug: "ada", Bio: "old"}}
	require.NoError(t, engine.Save(ctx, db, doc))

	found, err := engine.FindByID[TrackedNestedDoc](ctx, db, doc.ID)
	require.NoError(t, err)

	found.Profile.Bio = "new"
	changes, err := engine.GetChanges(db, found)
	require.NoError(t, err)

	// Contract: GetChanges reports nested mutations at the outer struct
	// field's JSON key, with the entire sub-object as before/after.
	// Callers needing field-level diff can compare the maps themselves.
	require.Contains(t, changes, "profile")
	assert.NotContains(t, changes, "profile.bio",
		"GetChanges intentionally does not flatten nested paths — change surfaces at the outer key")

	before, ok := changes["profile"].Before.(map[string]any)
	require.True(t, ok, "Before should be a map for a nested struct")
	assert.Equal(t, "old", before["bio"])

	after, ok := changes["profile"].After.(map[string]any)
	require.True(t, ok, "After should be a map for a nested struct")
	assert.Equal(t, "new", after["bio"])
	assert.Equal(t, "ada", after["slug"], "untouched nested fields are preserved in the After snapshot")
}

func TestRevert_NestedValueField(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedNestedDoc{})
	ctx := context.Background()

	doc := &TrackedNestedDoc{Email: "a@example.com", Profile: trackedProfile{Slug: "ada", Bio: "old"}}
	require.NoError(t, engine.Save(ctx, db, doc))

	found, err := engine.FindByID[TrackedNestedDoc](ctx, db, doc.ID)
	require.NoError(t, err)

	found.Profile.Bio = "new"
	found.Email = "b@example.com"
	require.NoError(t, engine.Revert(db, found))

	assert.Equal(t, "old", found.Profile.Bio, "Revert restores nested-value field")
	assert.Equal(t, "a@example.com", found.Email, "Revert also restores sibling top-level fields")

	changed, err := engine.IsChanged(db, found)
	require.NoError(t, err)
	assert.False(t, changed, "after Revert the doc must report no changes")
}

func TestIsChanged_NestedPointerField(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedNestedPtrDoc{})
	ctx := context.Background()

	doc := &TrackedNestedPtrDoc{Email: "a@example.com", Profile: &trackedProfile{Slug: "ada", Bio: "old"}}
	require.NoError(t, engine.Save(ctx, db, doc))

	found, err := engine.FindByID[TrackedNestedPtrDoc](ctx, db, doc.ID)
	require.NoError(t, err)

	// Nil → non-nil and value mutation are both detected.
	found.Profile.Bio = "new"
	changed, err := engine.IsChanged(db, found)
	require.NoError(t, err)
	assert.True(t, changed, "modifying a field through a non-nil pointer surfaces as changed")
}

func TestRevert_NestedPointerNilTransition(t *testing.T) {
	db := dentest.MustOpen(t, &TrackedNestedPtrDoc{})
	ctx := context.Background()

	doc := &TrackedNestedPtrDoc{Email: "a@example.com"} // nil Profile at save
	require.NoError(t, engine.Save(ctx, db, doc))

	found, err := engine.FindByID[TrackedNestedPtrDoc](ctx, db, doc.ID)
	require.NoError(t, err)

	found.Profile = &trackedProfile{Slug: "ada"}
	require.NoError(t, engine.Revert(db, found))
	assert.Nil(t, found.Profile, "Revert restores the nil-pointer state")
}
