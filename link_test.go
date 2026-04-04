package den_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/where"
)

type Door struct {
	document.Base
	Height int `json:"height"`
	Width  int `json:"width"`
}

type Window struct {
	document.Base
	X int `json:"x"`
	Y int `json:"y"`
}

type House struct {
	document.Base
	Name    string             `json:"name"`
	Door    den.Link[Door]     `json:"door"`
	Windows []den.Link[Window] `json:"windows"`
}

func TestNewLink(t *testing.T) {
	d := &Door{Height: 200, Width: 80}
	d.ID = "door-1"

	link := den.NewLink(d)
	assert.Equal(t, "door-1", link.ID)
	assert.Equal(t, d, link.Value)
	assert.True(t, link.IsLoaded())
}

func TestLink_ZeroValue(t *testing.T) {
	var link den.Link[Door]
	assert.Empty(t, link.ID)
	assert.Nil(t, link.Value)
	assert.False(t, link.IsLoaded())
}

func TestLink_Serialization(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))

	w1 := &Window{X: 100, Y: 50}
	w2 := &Window{X: 200, Y: 50}
	require.NoError(t, den.Insert(ctx, db, w1))
	require.NoError(t, den.Insert(ctx, db, w2))

	house := &House{
		Name:    "Lakehouse",
		Door:    den.NewLink(door),
		Windows: []den.Link[Window]{den.NewLink(w1), den.NewLink(w2)},
	}
	require.NoError(t, den.Insert(ctx, db, house))

	// Retrieve — links should contain only IDs (lazy by default)
	found, err := den.FindByID[House](ctx, db, house.ID)
	require.NoError(t, err)
	assert.Equal(t, door.ID, found.Door.ID)
	assert.Nil(t, found.Door.Value, "lazy load should not resolve value")
	assert.False(t, found.Door.IsLoaded())
	require.Len(t, found.Windows, 2)
	assert.Equal(t, w1.ID, found.Windows[0].ID)
	assert.Equal(t, w2.ID, found.Windows[1].ID)
}

func TestFetchLink(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))

	house := &House{
		Name: "Cottage",
		Door: den.NewLink(door),
	}
	require.NoError(t, den.Insert(ctx, db, house))

	found, err := den.FindByID[House](ctx, db, house.ID)
	require.NoError(t, err)
	assert.False(t, found.Door.IsLoaded())

	require.NoError(t, den.FetchLink(ctx, db, found, "door"))
	assert.True(t, found.Door.IsLoaded())
	require.NotNil(t, found.Door.Value)
	assert.Equal(t, 200, found.Door.Value.Height)
}

func TestFetchLink_SliceLink(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	w1 := &Window{X: 10, Y: 20}
	w2 := &Window{X: 30, Y: 40}
	require.NoError(t, den.Insert(ctx, db, w1))
	require.NoError(t, den.Insert(ctx, db, w2))

	house := &House{
		Name:    "Villa",
		Windows: []den.Link[Window]{den.NewLink(w1), den.NewLink(w2)},
	}
	require.NoError(t, den.Insert(ctx, db, house))

	found, err := den.FindByID[House](ctx, db, house.ID)
	require.NoError(t, err)

	require.NoError(t, den.FetchLink(ctx, db, found, "windows"))
	require.Len(t, found.Windows, 2)
	assert.True(t, found.Windows[0].IsLoaded())
	assert.True(t, found.Windows[1].IsLoaded())
}

func TestFetchLink_NotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	house := &House{Name: "Empty"}
	require.NoError(t, den.Insert(ctx, db, house))

	err := den.FetchLink(ctx, db, house, "nonexistent")
	require.Error(t, err)
}

func TestFetchAllLinks(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	w1 := &Window{X: 10, Y: 20}
	require.NoError(t, den.Insert(ctx, db, door))
	require.NoError(t, den.Insert(ctx, db, w1))

	house := &House{
		Name:    "Villa",
		Door:    den.NewLink(door),
		Windows: []den.Link[Window]{den.NewLink(w1)},
	}
	require.NoError(t, den.Insert(ctx, db, house))

	found, err := den.FindByID[House](ctx, db, house.ID)
	require.NoError(t, err)

	require.NoError(t, den.FetchAllLinks(ctx, db, found))
	assert.True(t, found.Door.IsLoaded())
	assert.Equal(t, 200, found.Door.Value.Height)
	require.Len(t, found.Windows, 1)
	assert.True(t, found.Windows[0].IsLoaded())
	assert.Equal(t, 10, found.Windows[0].Value.X)
}

func TestWithFetchLinks_Eager(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))

	house := &House{
		Name: "Bungalow",
		Door: den.NewLink(door),
	}
	require.NoError(t, den.Insert(ctx, db, house))

	results, err := den.NewQuery[House](ctx, db,
		where.Field("name").Eq("Bungalow"),
	).WithFetchLinks().All()
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Door.IsLoaded())
	require.NotNil(t, results[0].Door.Value)
	assert.Equal(t, 200, results[0].Door.Value.Height)
}

func TestWithLinkRule_Write(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 300, Width: 100}
	house := &House{
		Name: "Mansion",
		Door: den.NewLink(door),
	}

	// Door has no ID yet — LinkWrite should cascade insert
	require.NoError(t, den.Insert(ctx, db, house, den.WithLinkRule(den.LinkWrite)))

	assert.NotEmpty(t, door.ID, "door should have been inserted")

	// Verify door was persisted
	foundDoor, err := den.FindByID[Door](ctx, db, door.ID)
	require.NoError(t, err)
	assert.Equal(t, 300, foundDoor.Height)
}

func TestWithLinkRule_Write_TransactionRollback(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	house := &House{
		Name: "TxCascade",
		Door: den.NewLink(door),
	}

	// Insert with cascade inside a transaction that rolls back
	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		if err := den.TxInsert(tx, house, den.WithLinkRule(den.LinkWrite)); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	require.Error(t, err)

	// Neither house nor door should exist after rollback
	_, err = den.FindByID[House](ctx, db, house.ID)
	require.ErrorIs(t, err, den.ErrNotFound, "house should not exist after rollback")

	if door.ID != "" {
		_, err = den.FindByID[Door](ctx, db, door.ID)
		require.ErrorIs(t, err, den.ErrNotFound, "cascaded door should not exist after rollback")
	}
}

func TestWithLinkRule_Write_SetsTimestamps(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	house := &House{
		Name: "Timestamps",
		Door: den.NewLink(door),
	}
	require.NoError(t, den.Insert(ctx, db, house, den.WithLinkRule(den.LinkWrite)))

	// Linked door should have timestamps set
	foundDoor, err := den.FindByID[Door](ctx, db, door.ID)
	require.NoError(t, err)
	assert.False(t, foundDoor.CreatedAt.IsZero(), "cascade-written doc should have CreatedAt")
	assert.False(t, foundDoor.UpdatedAt.IsZero(), "cascade-written doc should have UpdatedAt")
}

// InsertHookedPart is a linked type with insert hooks for cascade testing.
type InsertHookedPart struct {
	document.Base
	Label string `json:"label"`
}

var insertHookedPartBeforeInsertCalled bool
var insertHookedPartAfterInsertCalled bool

func (d *InsertHookedPart) BeforeInsert(_ context.Context) error {
	insertHookedPartBeforeInsertCalled = true
	return nil
}

func (d *InsertHookedPart) AfterInsert(_ context.Context) error {
	insertHookedPartAfterInsertCalled = true
	return nil
}

type InsertHookedAssembly struct {
	document.Base
	Name string                     `json:"name"`
	Part den.Link[InsertHookedPart] `json:"part"`
}

func TestWithLinkRule_Write_RunsInsertHooks(t *testing.T) {
	db := dentest.MustOpen(t, &InsertHookedPart{}, &InsertHookedAssembly{})
	ctx := context.Background()

	part := &InsertHookedPart{Label: "Engine"}
	assembly := &InsertHookedAssembly{
		Name: "Car",
		Part: den.NewLink(part),
	}

	insertHookedPartBeforeInsertCalled = false
	insertHookedPartAfterInsertCalled = false

	require.NoError(t, den.Insert(ctx, db, assembly, den.WithLinkRule(den.LinkWrite)))

	assert.True(t, insertHookedPartBeforeInsertCalled, "BeforeInsert should fire on cascade-written linked part")
	assert.True(t, insertHookedPartAfterInsertCalled, "AfterInsert should fire on cascade-written linked part")
}

func TestWithLinkRule_Delete(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))

	house := &House{
		Name: "Demolish",
		Door: den.NewLink(door),
	}
	require.NoError(t, den.Insert(ctx, db, house))

	require.NoError(t, den.Delete(ctx, db, house, den.WithLinkRule(den.LinkDelete)))

	// House gone
	_, err := den.FindByID[House](ctx, db, house.ID)
	require.ErrorIs(t, err, den.ErrNotFound)

	// Door also gone
	_, err = den.FindByID[Door](ctx, db, door.ID)
	require.ErrorIs(t, err, den.ErrNotFound)
}

func TestWithNestingDepth(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))

	house := &House{Name: "Depth", Door: den.NewLink(door)}
	require.NoError(t, den.Insert(ctx, db, house))

	results, err := den.NewQuery[House](ctx, db,
		where.Field("name").Eq("Depth"),
	).WithFetchLinks().WithNestingDepth(1).All()
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Door.IsLoaded())
}

func TestLink_MarshalJSON(t *testing.T) {
	link := den.NewLink(&Door{})
	link.ID = "door-1"

	data, err := link.MarshalJSON()
	require.NoError(t, err)
	assert.Equal(t, `"door-1"`, string(data))
}

func TestLink_UnmarshalJSON(t *testing.T) {
	var link den.Link[Door]
	err := link.UnmarshalJSON([]byte(`"door-42"`))
	require.NoError(t, err)
	assert.Equal(t, "door-42", link.ID)
	assert.False(t, link.IsLoaded())
}

// SoftDoor is a soft-deletable variant of Door for cascade tests.
type SoftDoor struct {
	document.SoftBase
	Height int `json:"height"`
	Width  int `json:"width"`
}

type SoftHouse struct {
	document.Base
	Name string             `json:"name"`
	Door den.Link[SoftDoor] `json:"door"`
}

func TestWithLinkRule_Delete_SoftDeleteLinked(t *testing.T) {
	db := dentest.MustOpen(t, &SoftDoor{}, &SoftHouse{})
	ctx := context.Background()

	door := &SoftDoor{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))

	house := &SoftHouse{
		Name: "SoftCascade",
		Door: den.NewLink(door),
	}
	require.NoError(t, den.Insert(ctx, db, house))

	// Cascade delete should soft-delete the linked door, not hard-delete it
	require.NoError(t, den.Delete(ctx, db, house, den.WithLinkRule(den.LinkDelete)))

	// House is hard-deleted (no SoftBase)
	_, err := den.FindByID[SoftHouse](ctx, db, house.ID)
	require.ErrorIs(t, err, den.ErrNotFound)

	// Door should still exist but be soft-deleted
	found, err := den.FindByID[SoftDoor](ctx, db, door.ID)
	require.NoError(t, err)
	assert.True(t, found.IsDeleted(), "linked door should be soft-deleted, not hard-deleted")
}

// HookedPart is a linked type with delete hooks for cascade testing.
type HookedPart struct {
	document.Base
	Label string `json:"label"`
}

var hookedPartBeforeDeleteCalled bool
var hookedPartAfterDeleteCalled bool

func (d *HookedPart) BeforeDelete(_ context.Context) error {
	hookedPartBeforeDeleteCalled = true
	return nil
}

func (d *HookedPart) AfterDelete(_ context.Context) error {
	hookedPartAfterDeleteCalled = true
	return nil
}

type HookedAssembly struct {
	document.Base
	Name string               `json:"name"`
	Part den.Link[HookedPart] `json:"part"`
}

func TestWithLinkRule_Delete_HooksOnLinked(t *testing.T) {
	db := dentest.MustOpen(t, &HookedPart{}, &HookedAssembly{})
	ctx := context.Background()

	part := &HookedPart{Label: "Motor"}
	require.NoError(t, den.Insert(ctx, db, part))

	assembly := &HookedAssembly{
		Name: "Machine",
		Part: den.NewLink(part),
	}
	require.NoError(t, den.Insert(ctx, db, assembly))

	hookedPartBeforeDeleteCalled = false
	hookedPartAfterDeleteCalled = false

	require.NoError(t, den.Delete(ctx, db, assembly, den.WithLinkRule(den.LinkDelete)))

	// Part is hard-deleted
	_, err := den.FindByID[HookedPart](ctx, db, part.ID)
	require.ErrorIs(t, err, den.ErrNotFound)

	// Hooks fired on the linked document
	assert.True(t, hookedPartBeforeDeleteCalled, "BeforeDelete should fire on cascaded linked part")
	assert.True(t, hookedPartAfterDeleteCalled, "AfterDelete should fire on cascaded linked part")
}

func TestWithLinkRule_Write_OnUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))

	house := &House{
		Name: "Home",
		Door: den.NewLink(door),
	}
	require.NoError(t, den.Insert(ctx, db, house))

	// Update door via cascade write
	house.Door.Value.Height = 250
	require.NoError(t, den.Update(ctx, db, house, den.WithLinkRule(den.LinkWrite)))

	// Door should be updated
	foundDoor, err := den.FindByID[Door](ctx, db, door.ID)
	require.NoError(t, err)
	assert.Equal(t, 250, foundDoor.Height)
}

func TestWithLinkRule_DeleteIgnore(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))

	house := &House{
		Name: "KeepDoor",
		Door: den.NewLink(door),
	}
	require.NoError(t, den.Insert(ctx, db, house))

	require.NoError(t, den.Delete(ctx, db, house, den.WithLinkRule(den.LinkIgnore)))

	// House gone
	_, err := den.FindByID[House](ctx, db, house.ID)
	require.ErrorIs(t, err, den.ErrNotFound)

	// Door still exists
	foundDoor, err := den.FindByID[Door](ctx, db, door.ID)
	require.NoError(t, err)
	assert.Equal(t, 200, foundDoor.Height)
}
