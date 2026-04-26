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

// TestNewLink_EmptyIDAllowed pins the cascade-write contract: a doc that
// has not been inserted yet (empty Base.ID) produces a Link with empty ID;
// LinkWrite later fills it in once the cascaded Insert assigns the ID.
// This must not panic.
func TestNewLink_EmptyIDAllowed(t *testing.T) {
	d := &Door{Height: 200, Width: 80} // no ID set
	link := den.NewLink(d)
	assert.Empty(t, link.ID)
	assert.Equal(t, d, link.Value)
	assert.True(t, link.IsLoaded())
}

// nestedBaseDoc embeds document.Base via an intermediate wrapper struct.
// FieldByName("ID") wouldn't find ID through this depth in the renamed-
// embed scenario; the type-walk does.
type nestedBaseWrapper struct {
	document.Base
	Extra string `json:"extra,omitempty"`
}

type nestedBaseDoc struct {
	nestedBaseWrapper
	Name string `json:"name"`
}

func TestNewLink_NestedEmbed(t *testing.T) {
	d := &nestedBaseDoc{Name: "x"}
	d.ID = "nested-1"

	link := den.NewLink(d)
	assert.Equal(t, "nested-1", link.ID,
		"NewLink must find document.Base through nested embeds, not just top-level promotion")
}

// noBaseDoc deliberately omits document.Base. Today NewLink silently
// returns Link{ID: ""} for this — a programmer error masquerading as a
// valid cascade-write input. The fix turns it into a panic so the
// misuse fails at construction time.
type noBaseDoc struct {
	Name string `json:"name"`
}

func TestNewLink_PanicsWithoutBase(t *testing.T) {
	d := &noBaseDoc{Name: "no-base"}
	assert.PanicsWithValue(t,
		"den: NewLink: type den_test.noBaseDoc does not embed document.Base",
		func() { _ = den.NewLink(d) },
		"NewLink on a type without document.Base must fail loudly, not silently produce a broken Link",
	)
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

// --- Per-field eager hydration via den:"eager" ---

// EagerHouse mixes eager and lazy link fields on the same type. Door
// is tagged eager → hydrated by default; Owner is untagged → stays
// lazy unless the caller opts into WithFetchLinks.
type EagerOwner struct {
	document.Base
	Name string `json:"name"`
}

type EagerHouse struct {
	document.Base
	Name  string               `json:"name"`
	Door  den.Link[Door]       `json:"door"  den:"eager"`
	Owner den.Link[EagerOwner] `json:"owner"`
}

// TestEagerLink_DefaultHydratesEagerField pins the new default mode:
// fields tagged `den:"eager"` are hydrated automatically; untagged
// fields stay lazy on the same query.
func TestEagerLink_DefaultHydratesEagerField(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))
	owner := &EagerOwner{Name: "Alice"}
	require.NoError(t, den.Insert(ctx, db, owner))

	h := &EagerHouse{
		Name:  "EagerCottage",
		Door:  den.NewLink(door),
		Owner: den.NewLink(owner),
	}
	require.NoError(t, den.Insert(ctx, db, h))

	results, err := den.NewQuery[EagerHouse](db).All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 1)
	got := results[0]

	assert.True(t, got.Door.IsLoaded(),
		"eager-tagged Door must be hydrated by default — that's the whole point of the tag")
	require.NotNil(t, got.Door.Value)
	assert.Equal(t, 200, got.Door.Value.Height)

	assert.False(t, got.Owner.IsLoaded(),
		"untagged Owner must stay lazy — eager doesn't bleed across fields on the same type")
	assert.Nil(t, got.Owner.Value)
}

// TestEagerLink_WithFetchLinksHydratesEverything pins that explicit
// WithFetchLinks() overrides the per-field decision and hydrates all
// link fields, eager-tagged or not.
func TestEagerLink_WithFetchLinksHydratesEverything(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))
	owner := &EagerOwner{Name: "Bob"}
	require.NoError(t, den.Insert(ctx, db, owner))
	require.NoError(t, den.Insert(ctx, db, &EagerHouse{
		Name: "FullCottage", Door: den.NewLink(door), Owner: den.NewLink(owner),
	}))

	results, err := den.NewQuery[EagerHouse](db).WithFetchLinks().All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 1)
	got := results[0]

	assert.True(t, got.Door.IsLoaded())
	assert.True(t, got.Owner.IsLoaded(),
		"WithFetchLinks must hydrate the untagged field too")
}

// TestEagerLink_WithoutFetchLinksSuppressesEager pins the bulk-export
// escape hatch: WithoutFetchLinks() turns off hydration entirely, even
// for eager-tagged fields.
func TestEagerLink_WithoutFetchLinksSuppressesEager(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))
	require.NoError(t, den.Insert(ctx, db, &EagerHouse{
		Name: "Bare", Door: den.NewLink(door),
	}))

	results, err := den.NewQuery[EagerHouse](db).WithoutFetchLinks().All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 1)
	got := results[0]

	assert.False(t, got.Door.IsLoaded(),
		"WithoutFetchLinks must override the eager tag — explicit beats schema default")
	assert.Equal(t, door.ID, got.Door.ID, "ID stays populated even without hydration")
	assert.Nil(t, got.Door.Value)
}

// TestEagerLink_IterRespectsEager pins that the per-row Iter path
// also respects eager tags (single-row hydration; no batching).
func TestEagerLink_IterRespectsEager(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))
	require.NoError(t, den.Insert(ctx, db, &EagerHouse{
		Name: "IterCottage", Door: den.NewLink(door),
	}))

	for got, err := range den.NewQuery[EagerHouse](db).Iter(ctx) {
		require.NoError(t, err)
		assert.True(t, got.Door.IsLoaded(),
			"Iter must hydrate eager-tagged links per row")
		assert.False(t, got.Owner.IsLoaded(),
			"Iter must skip untagged links by default")
	}
}

// TestFetchLinkField pins the typed alternative to FetchLink: pass a
// pointer to the Link[T] directly, no string field name lookup. Renames
// of the JSON tag on the parent's link field cannot silently break a
// FetchLinkField call the way they break FetchLink.
func TestFetchLinkField(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))

	house := &House{Name: "Cottage", Door: den.NewLink(door)}
	require.NoError(t, den.Insert(ctx, db, house))

	found, err := den.FindByID[House](ctx, db, house.ID)
	require.NoError(t, err)
	assert.False(t, found.Door.IsLoaded(), "Link should start unloaded after FindByID")

	require.NoError(t, den.FetchLinkField(ctx, db, &found.Door))
	assert.True(t, found.Door.IsLoaded())
	require.NotNil(t, found.Door.Value)
	assert.Equal(t, 200, found.Door.Value.Height)
}

// TestFetchLinkField_AlreadyLoaded is a no-op when the Link is already
// loaded. Mirrors FetchLink's idempotency.
func TestFetchLinkField_AlreadyLoaded(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))

	link := den.NewLink(door) // Loaded=true, Value=door
	require.True(t, link.IsLoaded())
	require.NoError(t, den.FetchLinkField(ctx, db, &link))
	assert.Same(t, door, link.Value, "FetchLinkField on a loaded Link must not replace Value")
}

// TestFetchLinkField_EmptyID is a no-op when the Link has no ID — same
// contract as the cascade-write path.
func TestFetchLinkField_EmptyID(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	var link den.Link[Door]
	require.NoError(t, den.FetchLinkField(ctx, db, &link))
	assert.False(t, link.IsLoaded())
	assert.Nil(t, link.Value)
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

	results, err := den.NewQuery[House](db,
		where.Field("name").Eq("Bungalow"),
	).WithFetchLinks().All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Door.IsLoaded())
	require.NotNil(t, results[0].Door.Value)
	assert.Equal(t, 200, results[0].Door.Value.Height)
}

func TestWithFetchLinks_BatchDedup(t *testing.T) {
	// Three parents, two distinct targets: door1 on Cabins A and C, door2 on B.
	// With batched resolution we expect parents referencing the same target
	// to share the decoded *Door pointer — a direct observable signal that
	// the query ran in batched mode rather than per-row.
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door1 := &Door{Height: 200, Width: 80}
	door2 := &Door{Height: 210, Width: 85}
	require.NoError(t, den.Insert(ctx, db, door1))
	require.NoError(t, den.Insert(ctx, db, door2))

	cabinA := &House{Name: "BatchA", Door: den.NewLink(door1)}
	cabinB := &House{Name: "BatchB", Door: den.NewLink(door2)}
	cabinC := &House{Name: "BatchC", Door: den.NewLink(door1)}
	require.NoError(t, den.InsertMany(ctx, db, []*House{cabinA, cabinB, cabinC}))

	results, err := den.NewQuery[House](db).
		Where(where.Field("name").In("BatchA", "BatchB", "BatchC")).
		Sort("name", den.Asc).
		WithFetchLinks().
		All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 3)
	for _, r := range results {
		require.True(t, r.Door.IsLoaded(), "every link must be loaded")
		require.NotNil(t, r.Door.Value)
	}
	// A and C share door1 — dedup means they reference the same *Door pointer.
	assert.Same(t, results[0].Door.Value, results[2].Door.Value,
		"parents sharing a link ID must share the decoded pointer")
	// B points at door2, distinct from door1.
	assert.NotSame(t, results[0].Door.Value, results[1].Door.Value)
	assert.Equal(t, 210, results[1].Door.Value.Height)
}

func TestWithFetchLinks_SliceField(t *testing.T) {
	// Slice-of-Link fields must also be batch-resolved, with two rows sharing
	// the same window fetched from a single IN-query.
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	w1 := &Window{X: 1, Y: 1}
	w2 := &Window{X: 2, Y: 2}
	require.NoError(t, den.Insert(ctx, db, w1))
	require.NoError(t, den.Insert(ctx, db, w2))

	house1 := &House{
		Name:    "SliceA",
		Windows: []den.Link[Window]{den.NewLink(w1), den.NewLink(w2)},
	}
	house2 := &House{
		Name:    "SliceB",
		Windows: []den.Link[Window]{den.NewLink(w1)}, // shares w1 with house1
	}
	require.NoError(t, den.InsertMany(ctx, db, []*House{house1, house2}))

	results, err := den.NewQuery[House](db).
		Where(where.Field("name").In("SliceA", "SliceB")).
		Sort("name", den.Asc).
		WithFetchLinks().
		All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Len(t, results[0].Windows, 2)
	require.Len(t, results[1].Windows, 1)
	for _, w := range results[0].Windows {
		assert.True(t, w.IsLoaded())
	}
	assert.True(t, results[1].Windows[0].IsLoaded())
	// The w1 instance is shared between house1[0] and house2[0].
	assert.Same(t, results[0].Windows[0].Value, results[1].Windows[0].Value)
}

// A and B are a two-hop chain so we can verify WithNestingDepth actually
// resolves links on the target documents too (not just the direct parent).
type NestLeaf struct {
	document.Base
	Label string `json:"label"`
}

type NestMid struct {
	document.Base
	Tag  string             `json:"tag"`
	Leaf den.Link[NestLeaf] `json:"leaf"`
}

type NestRoot struct {
	document.Base
	Name string            `json:"name"`
	Mid  den.Link[NestMid] `json:"mid"`
}

func TestWithFetchLinks_DanglingLinkErrors(t *testing.T) {
	// The batched path must preserve the per-row implementation's strictness:
	// if a parent references a link id that does not exist in the target
	// collection, .All() returns ErrNotFound. Otherwise callers migrating
	// from the old implementation would silently see Loaded=false.
	//
	// The error is also a *DanglingLinkError so callers can extract the
	// broken (collection, id) without parsing the message.
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))

	good := &House{Name: "Good", Door: den.NewLink(door)}
	bad := &House{Name: "Dangling", Door: den.Link[Door]{ID: "does-not-exist"}}
	require.NoError(t, den.InsertMany(ctx, db, []*House{good, bad}))

	_, err := den.NewQuery[House](db).
		Where(where.Field("name").In("Good", "Dangling")).
		WithFetchLinks().
		All(ctx)
	require.ErrorIs(t, err, den.ErrNotFound)

	var dle *den.DanglingLinkError
	require.ErrorAs(t, err, &dle, "must surface as the typed *DanglingLinkError")
	assert.Equal(t, "door", dle.Collection)
	assert.Equal(t, "does-not-exist", dle.ID)
}

func TestWithFetchLinks_NestedDepthTwo(t *testing.T) {
	db := dentest.MustOpen(t, &NestLeaf{}, &NestMid{}, &NestRoot{})
	ctx := context.Background()

	leaf := &NestLeaf{Label: "bottom"}
	require.NoError(t, den.Insert(ctx, db, leaf))

	mid := &NestMid{Tag: "middle", Leaf: den.NewLink(leaf)}
	require.NoError(t, den.Insert(ctx, db, mid))

	root := &NestRoot{Name: "top", Mid: den.NewLink(mid)}
	require.NoError(t, den.Insert(ctx, db, root))

	results, err := den.NewQuery[NestRoot](db).
		Where(where.Field("name").Eq("top")).
		WithFetchLinks().
		WithNestingDepth(2).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 1)

	require.True(t, results[0].Mid.IsLoaded(), "level 1 (NestMid) must be loaded")
	require.NotNil(t, results[0].Mid.Value)
	// Level 2: the loaded NestMid has a Leaf link that must also be resolved.
	require.True(t, results[0].Mid.Value.Leaf.IsLoaded(), "level 2 (NestLeaf) must be loaded")
	require.NotNil(t, results[0].Mid.Value.Leaf.Value)
	assert.Equal(t, "bottom", results[0].Mid.Value.Leaf.Value.Label)
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
		if err := den.Insert(ctx, tx, house, den.WithLinkRule(den.LinkWrite)); err != nil {
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

	results, err := den.NewQuery[House](db,
		where.Field("name").Eq("Depth"),
	).WithFetchLinks().WithNestingDepth(1).All(ctx)
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
	document.Base
	document.SoftDelete
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

	// House is hard-deleted (no SoftDelete embed)
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

// SoftHookedDoor is a soft-deletable cascade target that records which of
// the soft-delete-specific hooks fire so the cascade path can be verified
// to match the top-level delete flow.
type SoftHookedDoor struct {
	document.Base
	document.SoftDelete
	Label string `json:"label"`
}

var softHookedBeforeSoftDeleteCalled bool
var softHookedAfterSoftDeleteCalled bool

func (d *SoftHookedDoor) BeforeSoftDelete(_ context.Context) error {
	softHookedBeforeSoftDeleteCalled = true
	return nil
}

func (d *SoftHookedDoor) AfterSoftDelete(_ context.Context) error {
	softHookedAfterSoftDeleteCalled = true
	return nil
}

type SoftHookedHouse struct {
	document.Base
	Name string                   `json:"name"`
	Door den.Link[SoftHookedDoor] `json:"door"`
}

func TestWithLinkRule_Delete_FiresSoftDeleteHooksOnLinked(t *testing.T) {
	db := dentest.MustOpen(t, &SoftHookedDoor{}, &SoftHookedHouse{})
	ctx := context.Background()

	door := &SoftHookedDoor{Label: "Main"}
	require.NoError(t, den.Insert(ctx, db, door))

	house := &SoftHookedHouse{Name: "H", Door: den.NewLink(door)}
	require.NoError(t, den.Insert(ctx, db, house))

	softHookedBeforeSoftDeleteCalled = false
	softHookedAfterSoftDeleteCalled = false

	require.NoError(t, den.Delete(ctx, db, house, den.WithLinkRule(den.LinkDelete)))

	assert.True(t, softHookedBeforeSoftDeleteCalled, "BeforeSoftDelete must fire on cascade soft-deleted linked doc")
	assert.True(t, softHookedAfterSoftDeleteCalled, "AfterSoftDelete must fire on cascade soft-deleted linked doc")
}

// TestParity_WithLinkRule_HardDelete_CascadeHardDeletesSoftLinked covers the
// regression where cascadeDeleteLinks ignored the caller's HardDelete()
// option: parents were physically removed but linked targets that embed
// document.SoftDelete remained as soft-deleted ghost rows. Both backends
// share the cascade code, so a single parity test pins both ends.
func TestParity_WithLinkRule_HardDelete_CascadeHardDeletesSoftLinked(t *testing.T) {
	dbs := map[string]*den.DB{
		"sqlite":   dentest.MustOpen(t, &SoftDoor{}, &SoftHouse{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &SoftDoor{}, &SoftHouse{}),
	}

	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			door := &SoftDoor{Height: 200, Width: 80}
			require.NoError(t, den.Insert(ctx, db, door))

			house := &SoftHouse{Name: "HardCascade", Door: den.NewLink(door)}
			require.NoError(t, den.Insert(ctx, db, house))

			require.NoError(t, den.Delete(ctx, db, house,
				den.HardDelete(),
				den.WithLinkRule(den.LinkDelete),
			))

			// House gone (it has no SoftDelete embed, hard always anyway).
			_, err := den.FindByID[SoftHouse](ctx, db, house.ID)
			require.ErrorIs(t, err, den.ErrNotFound)

			// Door must be physically gone — not findable even with IncludeDeleted.
			results, err := den.NewQuery[SoftDoor](db).IncludeDeleted().All(ctx)
			require.NoError(t, err)
			assert.Empty(t, results,
				"linked SoftDoor should be hard-deleted; IncludeDeleted must not return it")
		})
	}
}

// SoftSkipHookedDoor mirrors SoftHookedDoor but with its own hook counters,
// so the hard-cascade test can assert "soft-delete hooks did NOT fire"
// without racing against TestWithLinkRule_Delete_FiresSoftDeleteHooksOnLinked.
type SoftSkipHookedDoor struct {
	document.Base
	document.SoftDelete
	Label string `json:"label"`
}

var (
	softSkipBeforeSoftDeleteCalled bool
	softSkipAfterSoftDeleteCalled  bool
	softSkipBeforeDeleteCalled     bool
	softSkipAfterDeleteCalled      bool
)

func (d *SoftSkipHookedDoor) BeforeSoftDelete(_ context.Context) error {
	softSkipBeforeSoftDeleteCalled = true
	return nil
}

func (d *SoftSkipHookedDoor) AfterSoftDelete(_ context.Context) error {
	softSkipAfterSoftDeleteCalled = true
	return nil
}

func (d *SoftSkipHookedDoor) BeforeDelete(_ context.Context) error {
	softSkipBeforeDeleteCalled = true
	return nil
}

func (d *SoftSkipHookedDoor) AfterDelete(_ context.Context) error {
	softSkipAfterDeleteCalled = true
	return nil
}

type SoftSkipHouse struct {
	document.Base
	Name string                       `json:"name"`
	Door den.Link[SoftSkipHookedDoor] `json:"door"`
}

// TestWithLinkRule_HardDelete_SkipsSoftDeleteHooksOnLinked pins the contract
// that on a hard-delete cascade against a SoftDelete-embedding linked target,
// only BeforeDelete + AfterDelete fire — the soft-delete-only hooks are
// skipped, matching deleteCore's behaviour for direct hard-deletes.
func TestWithLinkRule_HardDelete_SkipsSoftDeleteHooksOnLinked(t *testing.T) {
	db := dentest.MustOpen(t, &SoftSkipHookedDoor{}, &SoftSkipHouse{})
	ctx := context.Background()

	door := &SoftSkipHookedDoor{Label: "Main"}
	require.NoError(t, den.Insert(ctx, db, door))

	house := &SoftSkipHouse{Name: "H", Door: den.NewLink(door)}
	require.NoError(t, den.Insert(ctx, db, house))

	softSkipBeforeSoftDeleteCalled = false
	softSkipAfterSoftDeleteCalled = false
	softSkipBeforeDeleteCalled = false
	softSkipAfterDeleteCalled = false

	require.NoError(t, den.Delete(ctx, db, house,
		den.HardDelete(),
		den.WithLinkRule(den.LinkDelete),
	))

	assert.True(t, softSkipBeforeDeleteCalled, "BeforeDelete must fire on hard-cascade target")
	assert.True(t, softSkipAfterDeleteCalled, "AfterDelete must fire on hard-cascade target")
	assert.False(t, softSkipBeforeSoftDeleteCalled, "BeforeSoftDelete must NOT fire on hard-cascade")
	assert.False(t, softSkipAfterSoftDeleteCalled, "AfterSoftDelete must NOT fire on hard-cascade")
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

// ValidatedPart is a linked document with a custom Validate() method.
type ValidatedPart struct {
	document.Base
	Name string `json:"name"`
}

func (p *ValidatedPart) Validate(_ context.Context) error {
	if p.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

type Machine struct {
	document.Base
	Label string                    `json:"label"`
	Part  den.Link[ValidatedPart]   `json:"part"`
	Parts []den.Link[ValidatedPart] `json:"parts"`
}

func TestWithLinkRule_Write_RunsValidation(t *testing.T) {
	db := dentest.MustOpen(t, &ValidatedPart{}, &Machine{})
	ctx := context.Background()

	// Part with empty name should fail validation during cascade write
	invalidPart := &ValidatedPart{Name: ""}
	machine := &Machine{
		Label: "Drill",
		Part:  den.NewLink(invalidPart),
	}

	err := den.Insert(ctx, db, machine, den.WithLinkRule(den.LinkWrite))
	require.ErrorIs(t, err, den.ErrValidation)

	// Part with valid name should succeed
	validPart := &ValidatedPart{Name: "Motor"}
	machine2 := &Machine{
		Label: "Saw",
		Part:  den.NewLink(validPart),
	}

	require.NoError(t, den.Insert(ctx, db, machine2, den.WithLinkRule(den.LinkWrite)))
	assert.NotEmpty(t, validPart.ID)
}
