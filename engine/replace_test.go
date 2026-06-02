package engine_test

import (
	"github.com/oliverandrich/den/engine"

	"context"
	"testing"
	"time"

	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type replaceProduct struct {
	document.Base
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

type replaceRevProduct struct {
	document.Base
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

func (replaceRevProduct) DenSettings() engine.Settings {
	return engine.Settings{UseRevision: true}
}

type replaceSoftProduct struct {
	document.Base
	document.SoftDelete
	Name string `json:"name"`
}

func TestReplace_PreservesCreatedAt_ResetsOmitted(t *testing.T) {
	db := dentest.MustOpen(t, &replaceProduct{})
	ctx := context.Background()

	orig := &replaceProduct{Name: "a", Price: 5}
	require.NoError(t, engine.Save(ctx, db, orig))
	createdAt := orig.CreatedAt
	require.False(t, createdAt.IsZero())
	time.Sleep(2 * time.Millisecond)

	// Fresh doc carries only the id and a new name; Price omitted.
	fresh := &replaceProduct{Name: "b"}
	fresh.ID = orig.ID
	require.NoError(t, engine.Replace(ctx, db, fresh))

	got, err := engine.FindByID[replaceProduct](ctx, db, orig.ID)
	require.NoError(t, err)
	assert.Equal(t, "b", got.Name)
	assert.Zero(t, got.Price, "full replace resets the omitted field to zero")
	assert.Equal(t, orig.ID, got.ID, "id preserved")
	assert.Equal(t, createdAt.UnixNano(), got.CreatedAt.UnixNano(), "created_at preserved")
	assert.True(t, got.UpdatedAt.After(createdAt), "updated_at re-stamped")
}

func TestReplace_ErrNotFound(t *testing.T) {
	db := dentest.MustOpen(t, &replaceProduct{})
	ctx := context.Background()

	fresh := &replaceProduct{Name: "ghost"}
	fresh.ID = engine.NewID() // never persisted
	err := engine.Replace(ctx, db, fresh)
	require.ErrorIs(t, err, engine.ErrNotFound)
}

func TestReplace_RequiresID(t *testing.T) {
	db := dentest.MustOpen(t, &replaceProduct{})
	ctx := context.Background()

	err := engine.Replace(ctx, db, &replaceProduct{Name: "no id"})
	require.ErrorIs(t, err, engine.ErrValidation)
}

func TestReplace_RevisionedRoundTrip(t *testing.T) {
	dbs := map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &replaceRevProduct{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &replaceRevProduct{}),
	}
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			orig := &replaceRevProduct{Name: "a", Price: 1}
			require.NoError(t, engine.Save(ctx, db, orig))
			r0 := orig.Rev
			require.NotEmpty(t, r0)

			// Fresh doc has an EMPTY _rev — must not conflict (Replace carries
			// the stored rev onto it).
			fresh := &replaceRevProduct{Name: "b"}
			fresh.ID = orig.ID
			require.NoError(t, engine.Replace(ctx, db, fresh))

			got, err := engine.FindByID[replaceRevProduct](ctx, db, orig.ID)
			require.NoError(t, err)
			assert.Equal(t, "b", got.Name)
			assert.NotEmpty(t, got.Rev)
			assert.NotEqual(t, r0, got.Rev, "rev bumped by the replace")
		})
	}
}

func TestReplace_PreservesSoftDeleteState(t *testing.T) {
	dbs := map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &replaceSoftProduct{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &replaceSoftProduct{}),
	}
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			orig := &replaceSoftProduct{Name: "a"}
			require.NoError(t, engine.Save(ctx, db, orig))
			require.NoError(t, engine.Delete(ctx, db, orig)) // soft delete

			fresh := &replaceSoftProduct{Name: "b"}
			fresh.ID = orig.ID
			require.NoError(t, engine.Replace(ctx, db, fresh))

			got, err := engine.FindByID[replaceSoftProduct](ctx, db, orig.ID, engine.IncludeDeleted())
			require.NoError(t, err)
			assert.Equal(t, "b", got.Name, "content replaced")
			assert.True(t, got.IsDeleted(), "soft-delete state preserved — Replace does not resurrect")
		})
	}
}

func TestReplace_FiresUpdateHooks(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()

	seed := &orderingDoc{Name: "a"}
	require.NoError(t, engine.Save(ctx, db, seed))

	resetHookOrderCalls(t) // discard the insert-path hooks the seed fired

	fresh := &orderingDoc{Name: "b"}
	fresh.ID = seed.ID
	require.NoError(t, engine.Replace(ctx, db, fresh))

	assert.Equal(t, []string{"BeforeUpdate", "BeforeSave", "Validate", "AfterUpdate", "AfterSave"}, hookOrderCalls)
	assert.NotContains(t, hookOrderCalls, "BeforeInsert")
	assert.NotContains(t, hookOrderCalls, "AfterInsert")
}

func TestPreserveServerFields_WithSoftDelete(t *testing.T) {
	db := dentest.MustOpen(t, &replaceSoftProduct{})

	now := time.Now()
	src := &replaceSoftProduct{Name: "src"}
	src.ID = "id-src"
	src.CreatedAt = now
	src.UpdatedAt = now
	src.DeletedAt = &now
	src.DeletedBy = "alice"
	src.DeleteReason = "cleanup"

	dst := &replaceSoftProduct{Name: "dst-content"}
	require.NoError(t, engine.PreserveServerFields(db, dst, src))

	assert.Equal(t, "id-src", dst.ID)
	assert.Equal(t, now, dst.CreatedAt)
	assert.Equal(t, &now, dst.DeletedAt)
	assert.Equal(t, "alice", dst.DeletedBy)
	assert.Equal(t, "cleanup", dst.DeleteReason)
	assert.Equal(t, "dst-content", dst.Name, "client field untouched")
}

func TestPreserveServerFields_WithoutSoftDelete(t *testing.T) {
	db := dentest.MustOpen(t, &replaceProduct{})

	now := time.Now()
	src := &replaceProduct{Name: "src", Price: 9}
	src.ID = "id-src"
	src.CreatedAt = now

	dst := &replaceProduct{Name: "dst", Price: 1}
	require.NoError(t, engine.PreserveServerFields(db, dst, src))

	assert.Equal(t, "id-src", dst.ID)
	assert.Equal(t, now, dst.CreatedAt)
	assert.Equal(t, "dst", dst.Name, "client field untouched")
	assert.InDelta(t, 1.0, dst.Price, 0, "client field untouched")
}

func TestPreserveServerFields_UnregisteredType(t *testing.T) {
	db := dentest.MustOpen(t, &replaceProduct{})
	err := engine.PreserveServerFields(db, &replaceRevProduct{}, &replaceRevProduct{})
	require.ErrorIs(t, err, engine.ErrNotRegistered)
}
