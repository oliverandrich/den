package den_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
)

func TestBackLinks_SingleLink(t *testing.T) {
	for name, db := range map[string]*den.DB{
		"sqlite": dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200, Width: 80}
			require.NoError(t, den.Save(ctx, db, door))

			house := &House{Name: "Cottage", Door: den.NewLink(door)}
			require.NoError(t, den.Save(ctx, db, house))

			// Find all documents that link to this door
			links, err := den.NewQuery[House](db).BackLinks("door", door.ID).All(ctx)
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
			require.NoError(t, den.Save(ctx, db, door))

			h1 := &House{Name: "House A", Door: den.NewLink(door)}
			h2 := &House{Name: "House B", Door: den.NewLink(door)}
			require.NoError(t, den.Save(ctx, db, h1))
			require.NoError(t, den.Save(ctx, db, h2))

			links, err := den.NewQuery[House](db).BackLinks("door", door.ID).All(ctx)
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
			require.NoError(t, den.Save(ctx, db, door))

			links, err := den.NewQuery[House](db).BackLinks("door", door.ID).All(ctx)
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
			require.NoError(t, den.Save(ctx, db, door))

			house := &House{Name: "Temp", Door: den.NewLink(door)}
			require.NoError(t, den.Save(ctx, db, house))

			require.NoError(t, den.Delete(ctx, db, house))

			links, err := den.NewQuery[House](db).BackLinks("door", door.ID).All(ctx)
			require.NoError(t, err)
			assert.Empty(t, links)
		})
	}
}

// TestBackLinks_HonorsEagerTag pins that BackLinks (and BackLinksField
// via delegation) hydrates `den:"eager"`-tagged link fields on the
// returned holder type, and that WithoutFetchLinks suppresses it.
// EagerHouse is defined in link_test.go: Door is eager, Owner is lazy.
func TestBackLinks_HonorsEagerTag(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Save(ctx, db, door))
	require.NoError(t, den.Save(ctx, db, &EagerHouse{
		Name: "Cottage", Door: den.NewLink(door),
	}))

	got, err := den.NewQuery[EagerHouse](db).BackLinks("door", door.ID).All(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].Door.IsLoaded(),
		"BackLinks must honor den:\"eager\" on the holder type")

	gotSuppressed, err := den.NewQuery[EagerHouse](db).WithoutFetchLinks().BackLinks("door", door.ID).All(ctx)
	require.NoError(t, err)
	require.Len(t, gotSuppressed, 1)
	assert.False(t, gotSuppressed[0].Door.IsLoaded(),
		"WithoutFetchLinks must override the eager tag on BackLinks")
}
