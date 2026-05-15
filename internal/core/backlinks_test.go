package core_test

import (
	"github.com/oliverandrich/den/internal/core"

	"context"
	"testing"

	"github.com/oliverandrich/den/dentest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackLinks_SingleLink(t *testing.T) {
	for name, db := range map[string]*core.DB{
		"sqlite": dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200, Width: 80}
			require.NoError(t, core.Save(ctx, db, door))

			house := &House{Name: "Cottage", Door: core.NewLink(door)}
			require.NoError(t, core.Save(ctx, db, house))

			// Find all documents that link to this door
			links, err := core.NewQuery[House](db).BackLinks("door", door.ID).All(ctx)
			require.NoError(t, err)
			require.Len(t, links, 1)
			assert.Equal(t, house.ID, links[0].ID)
		})
	}
}

func TestBackLinks_MultipleLinks(t *testing.T) {
	for name, db := range map[string]*core.DB{
		"sqlite": dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200, Width: 80}
			require.NoError(t, core.Save(ctx, db, door))

			h1 := &House{Name: "House A", Door: core.NewLink(door)}
			h2 := &House{Name: "House B", Door: core.NewLink(door)}
			require.NoError(t, core.Save(ctx, db, h1))
			require.NoError(t, core.Save(ctx, db, h2))

			links, err := core.NewQuery[House](db).BackLinks("door", door.ID).All(ctx)
			require.NoError(t, err)
			assert.Len(t, links, 2)
		})
	}
}

func TestBackLinks_NoLinks(t *testing.T) {
	for name, db := range map[string]*core.DB{
		"sqlite": dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200, Width: 80}
			require.NoError(t, core.Save(ctx, db, door))

			links, err := core.NewQuery[House](db).BackLinks("door", door.ID).All(ctx)
			require.NoError(t, err)
			assert.Empty(t, links)
		})
	}
}

func TestBackLinks_DeleteRemovesLink(t *testing.T) {
	for name, db := range map[string]*core.DB{
		"sqlite": dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200, Width: 80}
			require.NoError(t, core.Save(ctx, db, door))

			house := &House{Name: "Temp", Door: core.NewLink(door)}
			require.NoError(t, core.Save(ctx, db, house))

			require.NoError(t, core.Delete(ctx, db, house))

			links, err := core.NewQuery[House](db).BackLinks("door", door.ID).All(ctx)
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
	require.NoError(t, core.Save(ctx, db, door))
	require.NoError(t, core.Save(ctx, db, &EagerHouse{
		Name: "Cottage", Door: core.NewLink(door),
	}))

	got, err := core.NewQuery[EagerHouse](db).BackLinks("door", door.ID).All(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].Door.IsLoaded(),
		"BackLinks must honor den:\"eager\" on the holder type")

	gotSuppressed, err := core.NewQuery[EagerHouse](db).WithoutFetchLinks().BackLinks("door", door.ID).All(ctx)
	require.NoError(t, err)
	require.Len(t, gotSuppressed, 1)
	assert.False(t, gotSuppressed[0].Door.IsLoaded(),
		"WithoutFetchLinks must override the eager tag on BackLinks")
}
