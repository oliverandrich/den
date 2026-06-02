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
		})
	}
}
