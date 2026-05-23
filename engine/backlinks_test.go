package engine_test

import (
	"github.com/oliverandrich/den/engine"

	"context"
	"testing"

	"github.com/oliverandrich/den/dentest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackLinks_SingleLink(t *testing.T) {
	for name, db := range map[string]*engine.DB{
		"sqlite": dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200, Width: 80}
			require.NoError(t, engine.Save(ctx, db, door))

			house := &House{Name: "Cottage", Door: engine.NewLink(door)}
			require.NoError(t, engine.Save(ctx, db, house))

			// Find all documents that link to this door
			links, err := engine.NewQuery[House](db).BackLinks("door", door.ID).All(ctx)
			require.NoError(t, err)
			require.Len(t, links, 1)
			assert.Equal(t, house.ID, links[0].ID)
		})
	}
}

func TestBackLinks_MultipleLinks(t *testing.T) {
	for name, db := range map[string]*engine.DB{
		"sqlite": dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200, Width: 80}
			require.NoError(t, engine.Save(ctx, db, door))

			h1 := &House{Name: "House A", Door: engine.NewLink(door)}
			h2 := &House{Name: "House B", Door: engine.NewLink(door)}
			require.NoError(t, engine.Save(ctx, db, h1))
			require.NoError(t, engine.Save(ctx, db, h2))

			links, err := engine.NewQuery[House](db).BackLinks("door", door.ID).All(ctx)
			require.NoError(t, err)
			assert.Len(t, links, 2)
		})
	}
}

func TestBackLinks_NoLinks(t *testing.T) {
	for name, db := range map[string]*engine.DB{
		"sqlite": dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200, Width: 80}
			require.NoError(t, engine.Save(ctx, db, door))

			links, err := engine.NewQuery[House](db).BackLinks("door", door.ID).All(ctx)
			require.NoError(t, err)
			assert.Empty(t, links)
		})
	}
}

func TestBackLinks_DeleteRemovesLink(t *testing.T) {
	for name, db := range map[string]*engine.DB{
		"sqlite": dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200, Width: 80}
			require.NoError(t, engine.Save(ctx, db, door))

			house := &House{Name: "Temp", Door: engine.NewLink(door)}
			require.NoError(t, engine.Save(ctx, db, house))

			require.NoError(t, engine.Delete(ctx, db, house))

			links, err := engine.NewQuery[House](db).BackLinks("door", door.ID).All(ctx)
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
	require.NoError(t, engine.Save(ctx, db, door))
	require.NoError(t, engine.Save(ctx, db, &EagerHouse{
		Name: "Cottage", Door: engine.NewLink(door),
	}))

	got, err := engine.NewQuery[EagerHouse](db).BackLinks("door", door.ID).All(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].Door.IsLoaded(),
		"BackLinks must honor den:\"eager\" on the holder type")

	gotSuppressed, err := engine.NewQuery[EagerHouse](db).WithoutFetchLinks().BackLinks("door", door.ID).All(ctx)
	require.NoError(t, err)
	require.Len(t, gotSuppressed, 1)
	assert.False(t, gotSuppressed[0].Door.IsLoaded(),
		"WithoutFetchLinks must override the eager tag on BackLinks")
}
