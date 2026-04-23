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

type SoftProduct struct {
	document.Base
	document.SoftDelete
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

func TestSoftDelete(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p := &SoftProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	require.NoError(t, den.Delete(ctx, db, p))
	assert.True(t, p.IsDeleted())

	// Should be hidden from normal queries
	results, err := den.NewQuery[SoftProduct](db).All(ctx)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestSoftDelete_IncludeDeleted(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p := &SoftProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))
	require.NoError(t, den.Delete(ctx, db, p))

	// IncludeDeleted shows soft-deleted docs
	results, err := den.NewQuery[SoftProduct](db).IncludeDeleted().All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.True(t, results[0].IsDeleted())
}

func TestSoftDelete_FindByID_StillAccessible(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p := &SoftProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))
	require.NoError(t, den.Delete(ctx, db, p))

	// FindByID still returns the document (it's a direct key lookup)
	found, err := den.FindByID[SoftProduct](ctx, db, p.ID)
	require.NoError(t, err)
	assert.True(t, found.IsDeleted())
}

func TestHardDelete(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p := &SoftProduct{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))
	require.NoError(t, den.Delete(ctx, db, p))

	// Hard delete permanently removes
	require.NoError(t, den.Delete(ctx, db, p, den.HardDelete()))

	_, err := den.FindByID[SoftProduct](ctx, db, p.ID)
	require.ErrorIs(t, err, den.ErrNotFound)
}

// TestSoftDelete_AuditFields_Defaults pins that a bare soft-delete without
// the audit-field options leaves DeletedBy and DeleteReason as the zero
// string — existing callers see no behavior change.
func TestSoftDelete_AuditFields_Defaults(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p := &SoftProduct{Name: "Widget"}
	require.NoError(t, den.Insert(ctx, db, p))
	require.NoError(t, den.Delete(ctx, db, p))

	assert.True(t, p.IsDeleted())
	assert.Empty(t, p.DeletedBy)
	assert.Empty(t, p.DeleteReason)
}

// TestSoftDelete_AuditFields_Populated confirms SoftDeleteBy and
// SoftDeleteReason record audit metadata on the stored document.
func TestSoftDelete_AuditFields_Populated(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p := &SoftProduct{Name: "Widget"}
	require.NoError(t, den.Insert(ctx, db, p))
	require.NoError(t, den.Delete(ctx, db, p,
		den.SoftDeleteBy("usr_42"),
		den.SoftDeleteReason("violated terms"),
	))

	assert.Equal(t, "usr_42", p.DeletedBy)
	assert.Equal(t, "violated terms", p.DeleteReason)

	found, err := den.FindByID[SoftProduct](ctx, db, p.ID)
	require.NoError(t, err)
	assert.True(t, found.IsDeleted())
	assert.Equal(t, "usr_42", found.DeletedBy)
	assert.Equal(t, "violated terms", found.DeleteReason)
}

// TestSoftDelete_AuditFields_IgnoredOnHardDelete confirms the soft-only
// options are silently no-ops on the hard-delete path (the row is gone,
// there is no place to store the audit data).
func TestSoftDelete_AuditFields_IgnoredOnHardDelete(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p := &SoftProduct{Name: "Widget"}
	require.NoError(t, den.Insert(ctx, db, p))
	require.NoError(t, den.Delete(ctx, db, p,
		den.HardDelete(),
		den.SoftDeleteBy("usr_42"),
		den.SoftDeleteReason("violated terms"),
	))

	_, err := den.FindByID[SoftProduct](ctx, db, p.ID)
	require.ErrorIs(t, err, den.ErrNotFound)
}

func TestSoftDelete_Count(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p1 := &SoftProduct{Name: "Keep", Price: 10.0}
	p2 := &SoftProduct{Name: "Delete", Price: 20.0}
	require.NoError(t, den.Insert(ctx, db, p1))
	require.NoError(t, den.Insert(ctx, db, p2))
	require.NoError(t, den.Delete(ctx, db, p2))

	count, err := den.NewQuery[SoftProduct](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}
