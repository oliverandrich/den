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

func TestBackLinks_SingleLink(t *testing.T) {
	for name, db := range map[string]*den.DB{
		"sqlite": dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200, Width: 80}
			require.NoError(t, den.Insert(ctx, db, door))

			house := &House{Name: "Cottage", Door: den.NewLink(door)}
			require.NoError(t, den.Insert(ctx, db, house))

			// Find all documents that link to this door
			links, err := den.BackLinks[House](ctx, db, "door", door.ID)
			require.NoError(t, err)
			require.Len(t, links, 1)
			assert.Equal(t, house.ID, links[0].ID)
		})
	}
}

func TestBackLinks_MultipleLinks(t *testing.T) {
	for name, db := range map[string]*den.DB{
		"sqlite": dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200, Width: 80}
			require.NoError(t, den.Insert(ctx, db, door))

			h1 := &House{Name: "House A", Door: den.NewLink(door)}
			h2 := &House{Name: "House B", Door: den.NewLink(door)}
			require.NoError(t, den.Insert(ctx, db, h1))
			require.NoError(t, den.Insert(ctx, db, h2))

			links, err := den.BackLinks[House](ctx, db, "door", door.ID)
			require.NoError(t, err)
			assert.Len(t, links, 2)
		})
	}
}

func TestBackLinks_NoLinks(t *testing.T) {
	for name, db := range map[string]*den.DB{
		"sqlite": dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200, Width: 80}
			require.NoError(t, den.Insert(ctx, db, door))

			links, err := den.BackLinks[House](ctx, db, "door", door.ID)
			require.NoError(t, err)
			assert.Empty(t, links)
		})
	}
}

func TestBackLinks_DeleteRemovesLink(t *testing.T) {
	for name, db := range map[string]*den.DB{
		"sqlite": dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200, Width: 80}
			require.NoError(t, den.Insert(ctx, db, door))

			house := &House{Name: "Temp", Door: den.NewLink(door)}
			require.NoError(t, den.Insert(ctx, db, house))

			require.NoError(t, den.Delete(ctx, db, house))

			links, err := den.BackLinks[House](ctx, db, "door", door.ID)
			require.NoError(t, err)
			assert.Empty(t, links)
		})
	}
}

// --- BackLinksField (typed variant) ---

// TestBackLinksField_HappyPath pins that BackLinksField finds the unique
// Link[Door] field on House by type, infers the JSON tag, and returns
// the same result as the string-based BackLinks would.
func TestBackLinksField_HappyPath(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))

	h1 := &House{Name: "A", Door: den.NewLink(door)}
	h2 := &House{Name: "B", Door: den.NewLink(door)}
	require.NoError(t, den.Insert(ctx, db, h1))
	require.NoError(t, den.Insert(ctx, db, h2))

	links, err := den.BackLinksField[House, Door](ctx, db, door.ID)
	require.NoError(t, err)
	assert.Len(t, links, 2)
}

// TestBackLinksField_RejectsSliceOnlyHolder pins the slice-link
// limitation: []Link[T] fields aren't supported by the typed lookup
// because the underlying BackLinks uses Eq (which doesn't match
// against array contents). The error has to point the user at the
// manual Contains query instead of returning silently empty results.
//
// House.Windows is the only []Link[Window] field; House has no bare
// Link[Window], so BackLinksField[House, Window] hits the slice-only
// branch.
func TestBackLinksField_RejectsSliceOnlyHolder(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	_, err := den.BackLinksField[House, Window](ctx, db, "any-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "House")
	assert.Contains(t, err.Error(), "Window")
	assert.Contains(t, err.Error(), "[]Link",
		"error must call out the slice-link form so the user knows why")
	assert.Contains(t, err.Error(), "Contains",
		"error must point at the Contains query as the manual workaround")
}

// linkFreeDoc has no Link[T] fields at all — used to exercise the
// "no matching link field" error path on BackLinksField.
type linkFreeDoc struct {
	document.Base
	Name string `json:"name"`
}

func TestBackLinksField_NoMatchingField(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &linkFreeDoc{})
	ctx := context.Background()

	_, err := den.BackLinksField[linkFreeDoc, Door](ctx, db, "any-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "linkFreeDoc")
	assert.Contains(t, err.Error(), "Door")
	assert.Contains(t, err.Error(), "no Link",
		"error must explain that the holder type has no Link[T] field for the target type")
}

// twoDoorHouse has two Link[Door] fields — exercises the ambiguity
// error path. The user has to fall back to the string-based BackLinks
// to disambiguate.
type twoDoorHouse struct {
	document.Base
	Name      string         `json:"name"`
	FrontDoor den.Link[Door] `json:"front_door"`
	BackDoor  den.Link[Door] `json:"back_door"`
}

func TestBackLinksField_AmbiguousMultipleLinkFields(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &twoDoorHouse{})
	ctx := context.Background()

	_, err := den.BackLinksField[twoDoorHouse, Door](ctx, db, "any-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "twoDoorHouse")
	assert.Contains(t, err.Error(), "front_door")
	assert.Contains(t, err.Error(), "back_door",
		"error must list every candidate field name to make disambiguation actionable")
	assert.Contains(t, err.Error(), "BackLinks",
		"error must point at the explicit-field-name BackLinks as the disambiguation tool")
}
