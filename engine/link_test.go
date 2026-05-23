package engine_test

import (
	"github.com/oliverandrich/den/engine"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/where"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	Name    string                `json:"name"`
	Door    engine.Link[Door]     `json:"door"`
	Windows []engine.Link[Window] `json:"windows"`
}

func TestNewLink(t *testing.T) {
	d := &Door{Height: 200, Width: 80}
	d.ID = "door-1"

	link := engine.NewLink(d)
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
	link := engine.NewLink(d)
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

	link := engine.NewLink(d)
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
		"den: NewLink: type engine_test.noBaseDoc does not embed document.Base",
		func() { _ = engine.NewLink(d) },
		"NewLink on a type without document.Base must fail loudly, not silently produce a broken Link",
	)
}

// namedBaseDoc carries document.Base as a NAMED field, not anonymous.
// extractBaseID only walks anonymous embeds — matching the rule
// util.AnalyzeStruct uses to populate StructInfo.BaseID — so a
// named-Base field does not count as a Base-bearing doc and NewLink
// must panic the same way it does for noBaseDoc.
type namedBaseDoc struct {
	Embedded document.Base
	Name     string `json:"name"`
}

func TestNewLink_PanicsOnNamedBase(t *testing.T) {
	d := &namedBaseDoc{Name: "named"}
	d.Embedded.ID = "should-not-be-found"
	assert.PanicsWithValue(t,
		"den: NewLink: type engine_test.namedBaseDoc does not embed document.Base",
		func() { _ = engine.NewLink(d) },
		"a named document.Base field is not an embed — extractBaseID and AnalyzeStruct must agree on this",
	)
}

func TestLink_ZeroValue(t *testing.T) {
	var link engine.Link[Door]
	assert.Empty(t, link.ID)
	assert.Nil(t, link.Value)
	assert.False(t, link.IsLoaded())
}

func TestLink_Serialization(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, engine.Save(ctx, db, door))

	w1 := &Window{X: 100, Y: 50}
	w2 := &Window{X: 200, Y: 50}
	require.NoError(t, engine.Save(ctx, db, w1))
	require.NoError(t, engine.Save(ctx, db, w2))

	house := &House{
		Name:    "Lakehouse",
		Door:    engine.NewLink(door),
		Windows: []engine.Link[Window]{engine.NewLink(w1), engine.NewLink(w2)},
	}
	require.NoError(t, engine.Save(ctx, db, house))

	// Retrieve — links should contain only IDs (lazy by default)
	found, err := engine.FindByID[House](ctx, db, house.ID)
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
	require.NoError(t, engine.Save(ctx, db, door))

	house := &House{
		Name: "Cottage",
		Door: engine.NewLink(door),
	}
	require.NoError(t, engine.Save(ctx, db, house))

	found, err := engine.FindByID[House](ctx, db, house.ID)
	require.NoError(t, err)
	assert.False(t, found.Door.IsLoaded())

	require.NoError(t, engine.FetchLink(ctx, db, found, "door"))
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
	Name  string                  `json:"name"`
	Door  engine.Link[Door]       `json:"door"  den:"eager"`
	Owner engine.Link[EagerOwner] `json:"owner"`
}

// TestEagerLink_DefaultHydratesEagerField pins the new default mode:
// fields tagged `den:"eager"` are hydrated automatically; untagged
// fields stay lazy on the same query.
func TestEagerLink_DefaultHydratesEagerField(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, engine.Save(ctx, db, door))
	owner := &EagerOwner{Name: "Alice"}
	require.NoError(t, engine.Save(ctx, db, owner))

	h := &EagerHouse{
		Name:  "EagerCottage",
		Door:  engine.NewLink(door),
		Owner: engine.NewLink(owner),
	}
	require.NoError(t, engine.Save(ctx, db, h))

	results, err := engine.NewQuery[EagerHouse](db).All(ctx)
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
	require.NoError(t, engine.Save(ctx, db, door))
	owner := &EagerOwner{Name: "Bob"}
	require.NoError(t, engine.Save(ctx, db, owner))
	require.NoError(t, engine.Save(ctx, db, &EagerHouse{
		Name: "FullCottage", Door: engine.NewLink(door), Owner: engine.NewLink(owner),
	}))

	results, err := engine.NewQuery[EagerHouse](db).WithFetchLinks().All(ctx)
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
	require.NoError(t, engine.Save(ctx, db, door))
	require.NoError(t, engine.Save(ctx, db, &EagerHouse{
		Name: "Bare", Door: engine.NewLink(door),
	}))

	results, err := engine.NewQuery[EagerHouse](db).WithoutFetchLinks().All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 1)
	got := results[0]

	assert.False(t, got.Door.IsLoaded(),
		"WithoutFetchLinks must override the eager tag — explicit beats schema default")
	assert.Equal(t, door.ID, got.Door.ID, "ID stays populated even without hydration")
	assert.Nil(t, got.Door.Value)
}

// TestEagerLink_IterRespectsEager pins that Iter respects eager tags on
// each streamed row.
func TestEagerLink_IterRespectsEager(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, engine.Save(ctx, db, door))
	require.NoError(t, engine.Save(ctx, db, &EagerHouse{
		Name: "IterCottage", Door: engine.NewLink(door),
	}))

	for got, err := range engine.NewQuery[EagerHouse](db).Iter(ctx) {
		require.NoError(t, err)
		assert.True(t, got.Door.IsLoaded(),
			"Iter must hydrate eager-tagged links per row")
		assert.False(t, got.Owner.IsLoaded(),
			"Iter must skip untagged links by default")
	}
}

// EagerSliceHouse pins that `[]Link[T]` honors `den:"eager"` too — the
// per-element walk inside batchResolveField must hit the same skip
// predicate as the scalar case.
type EagerSliceHouse struct {
	document.Base
	Name    string                `json:"name"`
	Windows []engine.Link[Window] `json:"windows" den:"eager"`
}

// TestEagerLink_SliceField pins that the slice-link path hydrates an
// eager-tagged `[]Link[T]` field by default. Previously only scalar
// `Link[T]` fields were exercised in the eager tests.
func TestEagerLink_SliceField(t *testing.T) {
	db := dentest.MustOpen(t, &Window{}, &EagerSliceHouse{})
	ctx := context.Background()

	w1 := &Window{X: 100, Y: 50}
	w2 := &Window{X: 200, Y: 60}
	require.NoError(t, engine.Save(ctx, db, w1))
	require.NoError(t, engine.Save(ctx, db, w2))
	h := &EagerSliceHouse{
		Name:    "SlicedCottage",
		Windows: []engine.Link[Window]{engine.NewLink(w1), engine.NewLink(w2)},
	}
	require.NoError(t, engine.Save(ctx, db, h))

	got, err := engine.FindByID[EagerSliceHouse](ctx, db, h.ID)
	require.NoError(t, err)
	require.Len(t, got.Windows, 2)
	for i, link := range got.Windows {
		assert.Truef(t, link.IsLoaded(),
			"slice element %d must be hydrated by the eager tag", i)
		require.NotNilf(t, link.Value, "slice element %d Value", i)
	}
}

// EagerInner / EagerMiddle / EagerOuter form a three-deep eager chain
// for nested-depth tests. Outer.Middle is eager → Middle.Inner is eager.
type EagerInner struct {
	document.Base
	Label string `json:"label"`
}

type EagerMiddle struct {
	document.Base
	Inner engine.Link[EagerInner] `json:"inner" den:"eager"`
}

type EagerOuter struct {
	document.Base
	Note   string                   `json:"note"`
	Middle engine.Link[EagerMiddle] `json:"middle" den:"eager"`
}

// TestEagerLink_NestedDepth pins the depth contract for recursive eager
// hydration. Both All (the batched terminal) and FindByID (the single-doc
// CRUD read) route through the same resolver, so both recurse up to
// nestDepth — the default depth resolves both levels. WithNestingDepth(1)
// caps recursion at the first level.
func TestEagerLink_NestedDepth(t *testing.T) {
	db := dentest.MustOpen(t, &EagerInner{}, &EagerMiddle{}, &EagerOuter{})
	ctx := context.Background()

	inner := &EagerInner{Label: "leaf"}
	require.NoError(t, engine.Save(ctx, db, inner))
	mid := &EagerMiddle{Inner: engine.NewLink(inner)}
	require.NoError(t, engine.Save(ctx, db, mid))
	outer := &EagerOuter{Middle: engine.NewLink(mid)}
	require.NoError(t, engine.Save(ctx, db, outer))

	t.Run("All recurses through nested eager under default depth", func(t *testing.T) {
		results, err := engine.NewQuery[EagerOuter](db,
			where.Field("_id").Eq(outer.ID),
		).All(ctx)
		require.NoError(t, err)
		require.Len(t, results, 1)
		got := results[0]
		require.True(t, got.Middle.IsLoaded(), "first level hydrated by eager")
		require.NotNil(t, got.Middle.Value)
		assert.True(t, got.Middle.Value.Inner.IsLoaded(),
			"batched path recurses into the loaded Middle's own eager links")
	})

	t.Run("All with WithNestingDepth(1) caps at first level", func(t *testing.T) {
		results, err := engine.NewQuery[EagerOuter](db,
			where.Field("_id").Eq(outer.ID),
		).WithNestingDepth(1).All(ctx)
		require.NoError(t, err)
		require.Len(t, results, 1)
		got := results[0]
		require.True(t, got.Middle.IsLoaded(), "first eager level fires")
		require.NotNil(t, got.Middle.Value)
		assert.False(t, got.Middle.Value.Inner.IsLoaded(),
			"WithNestingDepth(1) must stop before recursing into the second level")
	})

	t.Run("FindByID recurses uniformly with the batched terminals", func(t *testing.T) {
		got, err := engine.FindByID[EagerOuter](ctx, db, outer.ID)
		require.NoError(t, err)
		require.True(t, got.Middle.IsLoaded(), "first eager level fires")
		require.NotNil(t, got.Middle.Value)
		assert.True(t, got.Middle.Value.Inner.IsLoaded(),
			"FindByID routes through the same batched resolver as All — both levels load up to defaultNestingDepth")
	})
}

// TestEagerLink_AllReadTerminalsRecurse pins that every read terminal
// routes through the same batched resolver, so every terminal recurses up
// to nestDepth (or defaultNestingDepth for the non-QuerySet CRUD reads).
// FindByID is covered by TestEagerLink_NestedDepth. FetchAllLinks is the
// exception (fixed at depth=1 by API design) and is pinned by
// TestFetchAllLinks_SingleLevel.
func TestEagerLink_AllReadTerminalsRecurse(t *testing.T) {
	db := dentest.MustOpen(t, &EagerInner{}, &EagerMiddle{}, &EagerOuter{})
	ctx := context.Background()

	inner := &EagerInner{Label: "leaf"}
	require.NoError(t, engine.Save(ctx, db, inner))
	mid := &EagerMiddle{Inner: engine.NewLink(inner)}
	require.NoError(t, engine.Save(ctx, db, mid))
	outer := &EagerOuter{Note: "initial", Middle: engine.NewLink(mid)}
	require.NoError(t, engine.Save(ctx, db, outer))

	t.Run("Iter recurses with WithNestingDepth(2)", func(t *testing.T) {
		var got *EagerOuter
		for doc, err := range engine.NewQuery[EagerOuter](db,
			where.Field("_id").Eq(outer.ID),
		).WithFetchLinks().WithNestingDepth(2).Iter(ctx) {
			require.NoError(t, err)
			got = doc
		}
		require.NotNil(t, got)
		require.True(t, got.Middle.IsLoaded(), "first eager level fires on Iter")
		require.NotNil(t, got.Middle.Value)
		assert.True(t, got.Middle.Value.Inner.IsLoaded(),
			"Iter routes per-row through the batched resolver — WithNestingDepth(2) recurses")
	})

	t.Run("Refresh recurses up to defaultNestingDepth", func(t *testing.T) {
		doc := &EagerOuter{}
		doc.ID = outer.ID
		require.NoError(t, engine.Refresh(ctx, db, doc))
		require.True(t, doc.Middle.IsLoaded(), "first eager level fires on Refresh")
		require.NotNil(t, doc.Middle.Value)
		assert.True(t, doc.Middle.Value.Inner.IsLoaded(),
			"Refresh uses the same batched resolver path — both levels load")
	})

	t.Run("FindOneAndUpdate recurses", func(t *testing.T) {
		got, err := engine.NewQuery[EagerOuter](db, where.Field("_id").Eq(outer.ID)).UpdateOne(ctx, engine.SetFields{"note": "after-update"})
		require.NoError(t, err)
		require.True(t, got.Middle.IsLoaded(), "first eager level fires")
		require.NotNil(t, got.Middle.Value)
		assert.True(t, got.Middle.Value.Inner.IsLoaded(),
			"second eager level must load via the batched resolver path")
	})

	t.Run("FindOneAndUpsert update branch recurses", func(t *testing.T) {
		got, inserted, err := engine.NewQuery[EagerOuter](db, where.Field("_id").Eq(outer.ID)).UpsertOne(ctx, &EagerOuter{Note: "should-not-apply"}, engine.SetFields{"note": "via-upsert"})
		require.NoError(t, err)
		require.False(t, inserted, "row exists — must take update branch")
		require.True(t, got.Middle.IsLoaded(), "first eager level fires")
		require.NotNil(t, got.Middle.Value)
		assert.True(t, got.Middle.Value.Inner.IsLoaded(),
			"second eager level must load via the batched resolver path")
	})

	t.Run("FindOneAndUpsert insert branch recurses", func(t *testing.T) {
		freshInner := &EagerInner{Label: "leaf-2"}
		require.NoError(t, engine.Save(ctx, db, freshInner))
		freshMid := &EagerMiddle{Inner: engine.NewLink(freshInner)}
		require.NoError(t, engine.Save(ctx, db, freshMid))

		// Construct the link by ID only — engine.NewLink(freshMid) would carry
		// freshMid.Inner's pre-loaded state into the defaults and mask what
		// the system actually hydrates post-insert.
		defaults := &EagerOuter{
			Note:   "fresh",
			Middle: engine.Link[EagerMiddle]{ID: freshMid.ID},
		}

		got, inserted, err := engine.NewQuery[EagerOuter](db, where.Field("note").Eq("does-not-exist-yet")).UpsertOne(ctx, defaults, engine.SetFields{})
		require.NoError(t, err)
		require.True(t, inserted, "no match — must take insert branch")
		require.True(t, got.Middle.IsLoaded())
		require.NotNil(t, got.Middle.Value)
		assert.True(t, got.Middle.Value.Inner.IsLoaded(),
			"insert branch hydrates post-Insert through the same batched resolver — recurses")
	})
}

// SoftDeletableTarget is the link target that opts into soft-delete.
// Used by TestEagerLink_SoftDeletedTarget to pin what eager hydration
// does when the referenced row was soft-deleted.
type SoftDeletableTarget struct {
	document.Base
	document.SoftDelete
	Name string `json:"name"`
}

type HouseWithSoftLink struct {
	document.Base
	Name string                           `json:"name"`
	Ref  engine.Link[SoftDeletableTarget] `json:"ref" den:"eager"`
}

// TestEagerLink_SoftDeletedTarget pins the actual behavior for an eager
// link whose target has been soft-deleted: hydration uses Get/IN-query
// directly without applying the soft-delete filter, so the target IS
// loaded — Value points at the soft-deleted record. Callers can check
// `Ref.Value.IsDeleted()` to react. This matches FindByID's own
// soft-delete-blind contract (FindByID also returns soft-deleted docs
// by ID). Documented in relations.md "Eager + soft-delete".
func TestEagerLink_SoftDeletedTarget(t *testing.T) {
	db := dentest.MustOpen(t, &SoftDeletableTarget{}, &HouseWithSoftLink{})
	ctx := context.Background()

	target := &SoftDeletableTarget{Name: "doomed"}
	require.NoError(t, engine.Save(ctx, db, target))
	h := &HouseWithSoftLink{Name: "House", Ref: engine.NewLink(target)}
	require.NoError(t, engine.Save(ctx, db, h))

	require.NoError(t, engine.Delete(ctx, db, target))

	got, err := engine.FindByID[HouseWithSoftLink](ctx, db, h.ID)
	require.NoError(t, err, "the holder is fine; only the target was soft-deleted")
	assert.Equal(t, target.ID, got.Ref.ID, "ID survives even when target is soft-deleted")
	assert.True(t, got.Ref.IsLoaded(),
		"eager hydration loads soft-deleted targets — same contract as FindByID by ID, "+
			"the soft-delete filter is a query-time concept that doesn't apply to "+
			"by-ID link resolution")
	require.NotNil(t, got.Ref.Value)
	assert.True(t, got.Ref.Value.IsDeleted(),
		"caller can detect the soft-deleted state on the loaded target")
}

// TestFetchLinkField pins the typed alternative to FetchLink: pass a
// pointer to the Link[T] directly, no string field name lookup. Renames
// of the JSON tag on the parent's link field cannot silently break a
// FetchLinkField call the way they break FetchLink.
func TestFetchLinkField(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, engine.Save(ctx, db, door))

	house := &House{Name: "Cottage", Door: engine.NewLink(door)}
	require.NoError(t, engine.Save(ctx, db, house))

	found, err := engine.FindByID[House](ctx, db, house.ID)
	require.NoError(t, err)
	assert.False(t, found.Door.IsLoaded(), "Link should start unloaded after FindByID")

	require.NoError(t, engine.FetchLinkField(ctx, db, &found.Door))
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
	require.NoError(t, engine.Save(ctx, db, door))

	link := engine.NewLink(door) // Loaded=true, Value=door
	require.True(t, link.IsLoaded())
	require.NoError(t, engine.FetchLinkField(ctx, db, &link))
	assert.Same(t, door, link.Value, "FetchLinkField on a loaded Link must not replace Value")
}

// TestFetchLinkField_EmptyID is a no-op when the Link has no ID — same
// contract as the cascade-write path.
func TestFetchLinkField_EmptyID(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	var link engine.Link[Door]
	require.NoError(t, engine.FetchLinkField(ctx, db, &link))
	assert.False(t, link.IsLoaded())
	assert.Nil(t, link.Value)
}

func TestFetchLink_SliceLink(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	w1 := &Window{X: 10, Y: 20}
	w2 := &Window{X: 30, Y: 40}
	require.NoError(t, engine.Save(ctx, db, w1))
	require.NoError(t, engine.Save(ctx, db, w2))

	house := &House{
		Name:    "Villa",
		Windows: []engine.Link[Window]{engine.NewLink(w1), engine.NewLink(w2)},
	}
	require.NoError(t, engine.Save(ctx, db, house))

	found, err := engine.FindByID[House](ctx, db, house.ID)
	require.NoError(t, err)

	require.NoError(t, engine.FetchLink(ctx, db, found, "windows"))
	require.Len(t, found.Windows, 2)
	assert.True(t, found.Windows[0].IsLoaded())
	assert.True(t, found.Windows[1].IsLoaded())
}

func TestFetchLink_NotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	house := &House{Name: "Empty"}
	require.NoError(t, engine.Save(ctx, db, house))

	err := engine.FetchLink(ctx, db, house, "nonexistent")
	require.Error(t, err)
}

func TestFetchAllLinks(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	w1 := &Window{X: 10, Y: 20}
	require.NoError(t, engine.Save(ctx, db, door))
	require.NoError(t, engine.Save(ctx, db, w1))

	house := &House{
		Name:    "Villa",
		Door:    engine.NewLink(door),
		Windows: []engine.Link[Window]{engine.NewLink(w1)},
	}
	require.NoError(t, engine.Save(ctx, db, house))

	found, err := engine.FindByID[House](ctx, db, house.ID)
	require.NoError(t, err)

	require.NoError(t, engine.FetchAllLinks(ctx, db, found))
	assert.True(t, found.Door.IsLoaded())
	assert.Equal(t, 200, found.Door.Value.Height)
	require.Len(t, found.Windows, 1)
	assert.True(t, found.Windows[0].IsLoaded())
	assert.Equal(t, 10, found.Windows[0].Value.X)
}

// TestFetchAllLinks_SingleLevel pins FetchAllLinks' single-level contract.
// It hydrates the direct link fields on the doc it is called with, but
// does not recurse into the loaded targets — Mid is loaded, Mid.Value.Leaf
// stays unloaded. Callers needing transitive hydration should reach for a
// QuerySet terminal that routes through the batched resolver.
//
// Reuses the non-eager NestLeaf/NestMid/NestRoot chain so we observe what
// FetchAllLinks itself does, free of eager auto-hydration interference.
func TestFetchAllLinks_SingleLevel(t *testing.T) {
	db := dentest.MustOpen(t, &NestLeaf{}, &NestMid{}, &NestRoot{})
	ctx := context.Background()

	leaf := &NestLeaf{Label: "leaf"}
	require.NoError(t, engine.Save(ctx, db, leaf))
	mid := &NestMid{Leaf: engine.NewLink(leaf)}
	require.NoError(t, engine.Save(ctx, db, mid))
	root := &NestRoot{Mid: engine.NewLink(mid)}
	require.NoError(t, engine.Save(ctx, db, root))

	found, err := engine.FindByID[NestRoot](ctx, db, root.ID)
	require.NoError(t, err)
	require.False(t, found.Mid.IsLoaded(),
		"baseline: non-eager link must not be auto-hydrated by FindByID")

	require.NoError(t, engine.FetchAllLinks(ctx, db, found))
	require.True(t, found.Mid.IsLoaded(), "FetchAllLinks loads the direct link")
	require.NotNil(t, found.Mid.Value)
	assert.False(t, found.Mid.Value.Leaf.IsLoaded(),
		"FetchAllLinks is single-level; it does not recurse into the loaded target")
}

func TestWithFetchLinks_Eager(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, engine.Save(ctx, db, door))

	house := &House{
		Name: "Bungalow",
		Door: engine.NewLink(door),
	}
	require.NoError(t, engine.Save(ctx, db, house))

	results, err := engine.NewQuery[House](db,
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
	require.NoError(t, engine.Save(ctx, db, door1))
	require.NoError(t, engine.Save(ctx, db, door2))

	cabinA := &House{Name: "BatchA", Door: engine.NewLink(door1)}
	cabinB := &House{Name: "BatchB", Door: engine.NewLink(door2)}
	cabinC := &House{Name: "BatchC", Door: engine.NewLink(door1)}
	require.NoError(t, engine.SaveAll(ctx, db, []*House{cabinA, cabinB, cabinC}))

	results, err := engine.NewQuery[House](db).
		Where(where.Field("name").In("BatchA", "BatchB", "BatchC")).
		Sort("name", engine.Asc).
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
	require.NoError(t, engine.Save(ctx, db, w1))
	require.NoError(t, engine.Save(ctx, db, w2))

	house1 := &House{
		Name:    "SliceA",
		Windows: []engine.Link[Window]{engine.NewLink(w1), engine.NewLink(w2)},
	}
	house2 := &House{
		Name:    "SliceB",
		Windows: []engine.Link[Window]{engine.NewLink(w1)}, // shares w1 with house1
	}
	require.NoError(t, engine.SaveAll(ctx, db, []*House{house1, house2}))

	results, err := engine.NewQuery[House](db).
		Where(where.Field("name").In("SliceA", "SliceB")).
		Sort("name", engine.Asc).
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
	Tag  string                `json:"tag"`
	Leaf engine.Link[NestLeaf] `json:"leaf"`
}

type NestRoot struct {
	document.Base
	Name string               `json:"name"`
	Mid  engine.Link[NestMid] `json:"mid"`
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
	require.NoError(t, engine.Save(ctx, db, door))

	good := &House{Name: "Good", Door: engine.NewLink(door)}
	bad := &House{Name: "Dangling", Door: engine.Link[Door]{ID: "does-not-exist"}}
	require.NoError(t, engine.SaveAll(ctx, db, []*House{good, bad}))

	_, err := engine.NewQuery[House](db).
		Where(where.Field("name").In("Good", "Dangling")).
		WithFetchLinks().
		All(ctx)
	require.ErrorIs(t, err, engine.ErrNotFound)

	var dle *engine.DanglingLinkError
	require.ErrorAs(t, err, &dle, "must surface as the typed *DanglingLinkError")
	assert.Equal(t, "door", dle.Collection)
	assert.Equal(t, "does-not-exist", dle.ID)
}

func TestWithFetchLinks_NestedDepthTwo(t *testing.T) {
	db := dentest.MustOpen(t, &NestLeaf{}, &NestMid{}, &NestRoot{})
	ctx := context.Background()

	leaf := &NestLeaf{Label: "bottom"}
	require.NoError(t, engine.Save(ctx, db, leaf))

	mid := &NestMid{Tag: "middle", Leaf: engine.NewLink(leaf)}
	require.NoError(t, engine.Save(ctx, db, mid))

	root := &NestRoot{Name: "top", Mid: engine.NewLink(mid)}
	require.NoError(t, engine.Save(ctx, db, root))

	results, err := engine.NewQuery[NestRoot](db).
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
		Door: engine.NewLink(door),
	}

	// Door has no ID yet — LinkWrite should cascade insert
	require.NoError(t, engine.Save(ctx, db, house, engine.WithLinkRule(engine.LinkWrite)))

	assert.NotEmpty(t, door.ID, "door should have been inserted")

	// Verify door was persisted
	foundDoor, err := engine.FindByID[Door](ctx, db, door.ID)
	require.NoError(t, err)
	assert.Equal(t, 300, foundDoor.Height)
}

func TestWithLinkRule_Write_TransactionRollback(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	house := &House{
		Name: "TxCascade",
		Door: engine.NewLink(door),
	}

	// Insert with cascade inside a transaction that rolls back
	err := engine.RunInTransaction(ctx, db, func(tx *engine.Tx) error {
		if err := engine.Save(ctx, tx, house, engine.WithLinkRule(engine.LinkWrite)); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	require.Error(t, err)

	// Neither house nor door should exist after rollback
	_, err = engine.FindByID[House](ctx, db, house.ID)
	require.ErrorIs(t, err, engine.ErrNotFound, "house should not exist after rollback")

	if door.ID != "" {
		_, err = engine.FindByID[Door](ctx, db, door.ID)
		require.ErrorIs(t, err, engine.ErrNotFound, "cascaded door should not exist after rollback")
	}
}

func TestWithLinkRule_Write_SetsTimestamps(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	house := &House{
		Name: "Timestamps",
		Door: engine.NewLink(door),
	}
	require.NoError(t, engine.Save(ctx, db, house, engine.WithLinkRule(engine.LinkWrite)))

	// Linked door should have timestamps set
	foundDoor, err := engine.FindByID[Door](ctx, db, door.ID)
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
	Name string                        `json:"name"`
	Part engine.Link[InsertHookedPart] `json:"part"`
}

func TestWithLinkRule_Write_RunsInsertHooks(t *testing.T) {
	db := dentest.MustOpen(t, &InsertHookedPart{}, &InsertHookedAssembly{})
	ctx := context.Background()

	part := &InsertHookedPart{Label: "Engine"}
	assembly := &InsertHookedAssembly{
		Name: "Car",
		Part: engine.NewLink(part),
	}

	insertHookedPartBeforeInsertCalled = false
	insertHookedPartAfterInsertCalled = false

	require.NoError(t, engine.Save(ctx, db, assembly, engine.WithLinkRule(engine.LinkWrite)))

	assert.True(t, insertHookedPartBeforeInsertCalled, "BeforeInsert should fire on cascade-written linked part")
	assert.True(t, insertHookedPartAfterInsertCalled, "AfterInsert should fire on cascade-written linked part")
}

func TestWithLinkRule_Delete(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, engine.Save(ctx, db, door))

	house := &House{
		Name: "Demolish",
		Door: engine.NewLink(door),
	}
	require.NoError(t, engine.Save(ctx, db, house))

	require.NoError(t, engine.Delete(ctx, db, house, engine.WithLinkRule(engine.LinkDelete)))

	// House gone
	_, err := engine.FindByID[House](ctx, db, house.ID)
	require.ErrorIs(t, err, engine.ErrNotFound)

	// Door also gone
	_, err = engine.FindByID[Door](ctx, db, door.ID)
	require.ErrorIs(t, err, engine.ErrNotFound)
}

func TestWithNestingDepth(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, engine.Save(ctx, db, door))

	house := &House{Name: "Depth", Door: engine.NewLink(door)}
	require.NoError(t, engine.Save(ctx, db, house))

	results, err := engine.NewQuery[House](db,
		where.Field("name").Eq("Depth"),
	).WithFetchLinks().WithNestingDepth(1).All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Door.IsLoaded())
}

func TestLink_MarshalJSON(t *testing.T) {
	link := engine.NewLink(&Door{})
	link.ID = "door-1"

	data, err := link.MarshalJSON()
	require.NoError(t, err)
	assert.Equal(t, `"door-1"`, string(data))
}

// TestLink_MarshalJSON_GoccyParity pins the byte-for-byte invariant that
// the MarshalJSON fast path must preserve: whatever goccy would have
// produced for the same ID, the optimized path produces too. Anything
// that cannot be reproduced byte-exact falls through to goccy.
func TestLink_MarshalJSON_GoccyParity(t *testing.T) {
	cases := []string{
		"01HZK0NJK8T3K6F8KX27YZAQXV", // ULID — clean ASCII alphanumeric
		"plain-ascii-with-dashes",
		"with spaces and unicode ä",
		"",
		`with "quote"`,
		"with\nnewline",
		`with\backslash`,
		"with\ttab",
		"control\x01char",
	}
	for _, id := range cases {
		t.Run(fmt.Sprintf("%q", id), func(t *testing.T) {
			link := engine.Link[Door]{ID: id}
			got, err := link.MarshalJSON()
			require.NoError(t, err)

			want, err := json.Marshal(id)
			require.NoError(t, err)

			assert.Equal(t, string(want), string(got))
		})
	}
}

func TestLink_MarshalJSON_RoundTrip(t *testing.T) {
	cases := []string{
		"01HZK0NJK8T3K6F8KX27YZAQXV",
		`with "quote"`,
		"with\nnewline",
		`with\backslash`,
		"with\ttab",
		"control\x01char",
		"unicode ä",
		"",
	}
	for _, want := range cases {
		t.Run(fmt.Sprintf("%q", want), func(t *testing.T) {
			orig := engine.Link[Door]{ID: want}
			data, err := orig.MarshalJSON()
			require.NoError(t, err)

			var got engine.Link[Door]
			require.NoError(t, got.UnmarshalJSON(data))
			assert.Equal(t, want, got.ID)
		})
	}
}

func TestLink_UnmarshalJSON(t *testing.T) {
	var link engine.Link[Door]
	err := link.UnmarshalJSON([]byte(`"door-42"`))
	require.NoError(t, err)
	assert.Equal(t, "door-42", link.ID)
	assert.False(t, link.IsLoaded())
}

func TestLink_UnmarshalJSON_Null(t *testing.T) {
	// `null` is a valid JSON literal for a Link field. Den persists Links
	// as strings, but a hand-crafted payload with `null` must still
	// round-trip cleanly: ID resets to "", Loaded false.
	link := engine.Link[Door]{ID: "old", Loaded: true}
	err := link.UnmarshalJSON([]byte(`null`))
	require.NoError(t, err)
	assert.Empty(t, link.ID)
	assert.False(t, link.IsLoaded())
}

func TestLink_UnmarshalJSON_EscapeSequences(t *testing.T) {
	// IDs in production are ULIDs (no escapes), but the JSON contract
	// requires correct round-trip for any string. Pins escape handling so
	// the fast-path optimisation cannot silently corrupt unusual IDs.
	cases := []string{
		`with "quote"`,
		"with\nnewline",
		`with\backslash`,
		"with\ttab",
		`unicode ä`,
		"",
	}
	for _, want := range cases {
		t.Run(want, func(t *testing.T) {
			orig := engine.Link[Door]{ID: want}
			data, err := orig.MarshalJSON()
			require.NoError(t, err)

			var got engine.Link[Door]
			require.NoError(t, got.UnmarshalJSON(data))
			assert.Equal(t, want, got.ID)
			assert.False(t, got.IsLoaded())
		})
	}
}

func TestLink_UnmarshalJSON_RejectsMalformed(t *testing.T) {
	// Defensive contract: anything that isn't a JSON string (or `null`)
	// must surface an error rather than silently producing a zero Link.
	// Pre-seed the link to pin a stronger invariant: on error the link's
	// fields must stay untouched (no partial mutation).
	cases := []string{
		`123`,
		`true`,
		`[]`,
		`{"id":"x"}`,
		``,
		`"unterminated`,
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			link := engine.Link[Door]{ID: "sentinel", Loaded: true}
			err := link.UnmarshalJSON([]byte(in))
			require.Error(t, err)
			assert.Equal(t, "sentinel", link.ID, "ID must stay untouched on error")
			assert.True(t, link.IsLoaded(), "Loaded must stay untouched on error")
		})
	}
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
	Name string                `json:"name"`
	Door engine.Link[SoftDoor] `json:"door"`
}

func TestWithLinkRule_Delete_SoftDeleteLinked(t *testing.T) {
	db := dentest.MustOpen(t, &SoftDoor{}, &SoftHouse{})
	ctx := context.Background()

	door := &SoftDoor{Height: 200, Width: 80}
	require.NoError(t, engine.Save(ctx, db, door))

	house := &SoftHouse{
		Name: "SoftCascade",
		Door: engine.NewLink(door),
	}
	require.NoError(t, engine.Save(ctx, db, house))

	// Cascade delete should soft-delete the linked door, not hard-delete it
	require.NoError(t, engine.Delete(ctx, db, house, engine.WithLinkRule(engine.LinkDelete)))

	// House is hard-deleted (no SoftDelete embed)
	_, err := engine.FindByID[SoftHouse](ctx, db, house.ID)
	require.ErrorIs(t, err, engine.ErrNotFound)

	// Door should still exist but be soft-deleted
	found, err := engine.FindByID[SoftDoor](ctx, db, door.ID)
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
	Name string                  `json:"name"`
	Part engine.Link[HookedPart] `json:"part"`
}

func TestWithLinkRule_Delete_HooksOnLinked(t *testing.T) {
	db := dentest.MustOpen(t, &HookedPart{}, &HookedAssembly{})
	ctx := context.Background()

	part := &HookedPart{Label: "Motor"}
	require.NoError(t, engine.Save(ctx, db, part))

	assembly := &HookedAssembly{
		Name: "Machine",
		Part: engine.NewLink(part),
	}
	require.NoError(t, engine.Save(ctx, db, assembly))

	hookedPartBeforeDeleteCalled = false
	hookedPartAfterDeleteCalled = false

	require.NoError(t, engine.Delete(ctx, db, assembly, engine.WithLinkRule(engine.LinkDelete)))

	// Part is hard-deleted
	_, err := engine.FindByID[HookedPart](ctx, db, part.ID)
	require.ErrorIs(t, err, engine.ErrNotFound)

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
	Name string                      `json:"name"`
	Door engine.Link[SoftHookedDoor] `json:"door"`
}

func TestWithLinkRule_Delete_FiresSoftDeleteHooksOnLinked(t *testing.T) {
	db := dentest.MustOpen(t, &SoftHookedDoor{}, &SoftHookedHouse{})
	ctx := context.Background()

	door := &SoftHookedDoor{Label: "Main"}
	require.NoError(t, engine.Save(ctx, db, door))

	house := &SoftHookedHouse{Name: "H", Door: engine.NewLink(door)}
	require.NoError(t, engine.Save(ctx, db, house))

	softHookedBeforeSoftDeleteCalled = false
	softHookedAfterSoftDeleteCalled = false

	require.NoError(t, engine.Delete(ctx, db, house, engine.WithLinkRule(engine.LinkDelete)))

	assert.True(t, softHookedBeforeSoftDeleteCalled, "BeforeSoftDelete must fire on cascade soft-deleted linked doc")
	assert.True(t, softHookedAfterSoftDeleteCalled, "AfterSoftDelete must fire on cascade soft-deleted linked doc")
}

// TestParity_WithLinkRule_HardDelete_CascadeHardDeletesSoftLinked covers the
// regression where cascadeDeleteLinks ignored the caller's HardDelete()
// option: parents were physically removed but linked targets that embed
// document.SoftDelete remained as soft-deleted ghost rows. Both backends
// share the cascade code, so a single parity test pins both ends.
func TestParity_WithLinkRule_HardDelete_CascadeHardDeletesSoftLinked(t *testing.T) {
	dbs := map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &SoftDoor{}, &SoftHouse{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &SoftDoor{}, &SoftHouse{}),
	}

	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			door := &SoftDoor{Height: 200, Width: 80}
			require.NoError(t, engine.Save(ctx, db, door))

			house := &SoftHouse{Name: "HardCascade", Door: engine.NewLink(door)}
			require.NoError(t, engine.Save(ctx, db, house))

			require.NoError(t, engine.Delete(ctx, db, house,
				engine.HardDelete(),
				engine.WithLinkRule(engine.LinkDelete),
			))

			// House gone (it has no SoftDelete embed, hard always anyway).
			_, err := engine.FindByID[SoftHouse](ctx, db, house.ID)
			require.ErrorIs(t, err, engine.ErrNotFound)

			// Door must be physically gone — not findable even with IncludeDeleted.
			results, err := engine.NewQuery[SoftDoor](db).IncludeDeleted().All(ctx)
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
	Name string                          `json:"name"`
	Door engine.Link[SoftSkipHookedDoor] `json:"door"`
}

// TestWithLinkRule_HardDelete_SkipsSoftDeleteHooksOnLinked pins the contract
// that on a hard-delete cascade against a SoftDelete-embedding linked target,
// only BeforeDelete + AfterDelete fire — the soft-delete-only hooks are
// skipped, matching deleteCore's behaviour for direct hard-deletes.
func TestWithLinkRule_HardDelete_SkipsSoftDeleteHooksOnLinked(t *testing.T) {
	db := dentest.MustOpen(t, &SoftSkipHookedDoor{}, &SoftSkipHouse{})
	ctx := context.Background()

	door := &SoftSkipHookedDoor{Label: "Main"}
	require.NoError(t, engine.Save(ctx, db, door))

	house := &SoftSkipHouse{Name: "H", Door: engine.NewLink(door)}
	require.NoError(t, engine.Save(ctx, db, house))

	softSkipBeforeSoftDeleteCalled = false
	softSkipAfterSoftDeleteCalled = false
	softSkipBeforeDeleteCalled = false
	softSkipAfterDeleteCalled = false

	require.NoError(t, engine.Delete(ctx, db, house,
		engine.HardDelete(),
		engine.WithLinkRule(engine.LinkDelete),
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
	require.NoError(t, engine.Save(ctx, db, door))

	house := &House{
		Name: "Home",
		Door: engine.NewLink(door),
	}
	require.NoError(t, engine.Save(ctx, db, house))

	// Update door via cascade write
	house.Door.Value.Height = 250
	require.NoError(t, engine.Save(ctx, db, house, engine.WithLinkRule(engine.LinkWrite)))

	// Door should be updated
	foundDoor, err := engine.FindByID[Door](ctx, db, door.ID)
	require.NoError(t, err)
	assert.Equal(t, 250, foundDoor.Height)
}

func TestWithLinkRule_DeleteIgnore(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, engine.Save(ctx, db, door))

	house := &House{
		Name: "KeepDoor",
		Door: engine.NewLink(door),
	}
	require.NoError(t, engine.Save(ctx, db, house))

	require.NoError(t, engine.Delete(ctx, db, house, engine.WithLinkRule(engine.LinkIgnore)))

	// House gone
	_, err := engine.FindByID[House](ctx, db, house.ID)
	require.ErrorIs(t, err, engine.ErrNotFound)

	// Door still exists
	foundDoor, err := engine.FindByID[Door](ctx, db, door.ID)
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
	Label string                       `json:"label"`
	Part  engine.Link[ValidatedPart]   `json:"part"`
	Parts []engine.Link[ValidatedPart] `json:"parts"`
}

func TestWithLinkRule_Write_RunsValidation(t *testing.T) {
	db := dentest.MustOpen(t, &ValidatedPart{}, &Machine{})
	ctx := context.Background()

	// Part with empty name should fail validation during cascade write
	invalidPart := &ValidatedPart{Name: ""}
	machine := &Machine{
		Label: "Drill",
		Part:  engine.NewLink(invalidPart),
	}

	err := engine.Save(ctx, db, machine, engine.WithLinkRule(engine.LinkWrite))
	require.ErrorIs(t, err, engine.ErrValidation)

	// Part with valid name should succeed
	validPart := &ValidatedPart{Name: "Motor"}
	machine2 := &Machine{
		Label: "Saw",
		Part:  engine.NewLink(validPart),
	}

	require.NoError(t, engine.Save(ctx, db, machine2, engine.WithLinkRule(engine.LinkWrite)))
	assert.NotEmpty(t, validPart.ID)
}
