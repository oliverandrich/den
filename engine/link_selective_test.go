package engine_test

import (
	"github.com/oliverandrich/den/engine"

	"context"
	"testing"

	"github.com/oliverandrich/den/dentest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithFetchLinks_Selective pins that WithFetchLinks(fields...) hydrates
// only the named link fields, while the no-arg form still hydrates all — on
// both backends.
func TestWithFetchLinks_Selective(t *testing.T) {
	dbs := map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &Door{}, &Window{}, &House{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Door{}, &Window{}, &House{}),
	}
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			door := &Door{Height: 200}
			require.NoError(t, engine.Save(ctx, db, door))
			win := &Window{X: 1}
			require.NoError(t, engine.Save(ctx, db, win))
			h := &House{
				Name:    "Casa",
				Door:    engine.NewLink(door),
				Windows: []engine.Link[Window]{engine.NewLink(win)},
			}
			require.NoError(t, engine.Save(ctx, db, h))

			// Named: only door hydrates.
			named, err := engine.NewQuery[House](db).WithFetchLinks("door").All(ctx)
			require.NoError(t, err)
			require.Len(t, named, 1)
			assert.True(t, named[0].Door.IsLoaded(), "named link hydrated")
			require.Len(t, named[0].Windows, 1)
			assert.False(t, named[0].Windows[0].IsLoaded(), "unnamed link stays unloaded")

			// No-arg: everything hydrates.
			all, err := engine.NewQuery[House](db).WithFetchLinks().All(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1)
			assert.True(t, all[0].Door.IsLoaded())
			require.Len(t, all[0].Windows, 1)
			assert.True(t, all[0].Windows[0].IsLoaded(), "no-arg hydrates all links")

			// Unknown name: nothing hydrates, no error.
			none, err := engine.NewQuery[House](db).WithFetchLinks("nope").All(ctx)
			require.NoError(t, err)
			require.Len(t, none, 1)
			assert.False(t, none[0].Door.IsLoaded())

			// The selection is honored by every hydrating terminal, not just
			// All — First, AllWithCount and Iter route through the same
			// resolver (Search too, via the identical batchResolveLinks call).
			first, err := engine.NewQuery[House](db).WithFetchLinks("door").First(ctx)
			require.NoError(t, err)
			assert.True(t, first.Door.IsLoaded(), "First honors named hydration")
			require.Len(t, first.Windows, 1)
			assert.False(t, first.Windows[0].IsLoaded())

			withCount, _, err := engine.NewQuery[House](db).WithFetchLinks("door").AllWithCount(ctx)
			require.NoError(t, err)
			require.Len(t, withCount, 1)
			assert.True(t, withCount[0].Door.IsLoaded(), "AllWithCount honors named hydration")
			require.Len(t, withCount[0].Windows, 1)
			assert.False(t, withCount[0].Windows[0].IsLoaded())

			seen := 0
			for h, err := range engine.NewQuery[House](db).WithFetchLinks("door").Iter(ctx) {
				require.NoError(t, err)
				seen++
				assert.True(t, h.Door.IsLoaded(), "Iter honors named hydration")
				require.Len(t, h.Windows, 1)
				assert.False(t, h.Windows[0].IsLoaded())
			}
			assert.Equal(t, 1, seen)
		})
	}
}
