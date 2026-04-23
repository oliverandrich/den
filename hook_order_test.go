package den_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
)

// hookOrderCalls records every hook invocation in firing order across the
// freshly decoded instances bulk operations construct. Tests touching it
// must NOT use t.Parallel — there is no synchronization.
var hookOrderCalls []string

func resetHookOrderCalls(t *testing.T) {
	t.Helper()
	hookOrderCalls = nil
	t.Cleanup(func() { hookOrderCalls = nil })
}

// orderingDoc implements every lifecycle hook so tests can pin the exact
// firing order against one type. Each hook records its name and, if FailHook
// matches, returns an error so error-path semantics can be pinned too.
type orderingDoc struct {
	document.Base
	Name     string `json:"name"`
	FailHook string `json:"-"`
}

func (d *orderingDoc) record(name string) error {
	hookOrderCalls = append(hookOrderCalls, name)
	if d.FailHook == name {
		return errors.New("forced failure at " + name)
	}
	return nil
}

func (d *orderingDoc) BeforeInsert(_ context.Context) error { return d.record("BeforeInsert") }
func (d *orderingDoc) BeforeUpdate(_ context.Context) error { return d.record("BeforeUpdate") }
func (d *orderingDoc) BeforeDelete(_ context.Context) error { return d.record("BeforeDelete") }
func (d *orderingDoc) BeforeSave(_ context.Context) error   { return d.record("BeforeSave") }
func (d *orderingDoc) AfterInsert(_ context.Context) error  { return d.record("AfterInsert") }
func (d *orderingDoc) AfterUpdate(_ context.Context) error  { return d.record("AfterUpdate") }
func (d *orderingDoc) AfterDelete(_ context.Context) error  { return d.record("AfterDelete") }
func (d *orderingDoc) AfterSave(_ context.Context) error    { return d.record("AfterSave") }
func (d *orderingDoc) Validate() error                      { return d.record("Validate") }

// orderingSoftDoc mirrors orderingDoc but embeds SoftDelete so the soft-delete
// path can be exercised through the same recorder.
type orderingSoftDoc struct {
	document.Base
	document.SoftDelete
	Name     string `json:"name"`
	FailHook string `json:"-"`
}

func (d *orderingSoftDoc) record(name string) error {
	hookOrderCalls = append(hookOrderCalls, name)
	if d.FailHook == name {
		return errors.New("forced failure at " + name)
	}
	return nil
}

func (d *orderingSoftDoc) BeforeDelete(_ context.Context) error { return d.record("BeforeDelete") }
func (d *orderingSoftDoc) AfterDelete(_ context.Context) error  { return d.record("AfterDelete") }
func (d *orderingSoftDoc) BeforeSoftDelete(_ context.Context) error {
	return d.record("BeforeSoftDelete")
}
func (d *orderingSoftDoc) AfterSoftDelete(_ context.Context) error {
	return d.record("AfterSoftDelete")
}

// --- Insert ---

func TestHookOrder_Insert_FullChain(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()
	resetHookOrderCalls(t)

	require.NoError(t, den.Insert(ctx, db, &orderingDoc{Name: "x"}))
	assert.Equal(t,
		[]string{"BeforeInsert", "BeforeSave", "Validate", "AfterInsert", "AfterSave"},
		hookOrderCalls)
}

func TestHookOrder_Insert_BeforeInsertError_StopsImmediately(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()
	resetHookOrderCalls(t)

	err := den.Insert(ctx, db, &orderingDoc{Name: "x", FailHook: "BeforeInsert"})
	require.Error(t, err)
	assert.Equal(t, []string{"BeforeInsert"}, hookOrderCalls)

	count, err := den.NewQuery[orderingDoc](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count, "no document is written when BeforeInsert fails")
}

func TestHookOrder_Insert_BeforeSaveError_StopsBeforeValidate(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()
	resetHookOrderCalls(t)

	err := den.Insert(ctx, db, &orderingDoc{Name: "x", FailHook: "BeforeSave"})
	require.Error(t, err)
	assert.Equal(t, []string{"BeforeInsert", "BeforeSave"}, hookOrderCalls)
}

func TestHookOrder_Insert_ValidateError_StopsBeforeWrite(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()
	resetHookOrderCalls(t)

	err := den.Insert(ctx, db, &orderingDoc{Name: "x", FailHook: "Validate"})
	require.ErrorIs(t, err, den.ErrValidation)
	assert.Equal(t, []string{"BeforeInsert", "BeforeSave", "Validate"}, hookOrderCalls)

	count, err := den.NewQuery[orderingDoc](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestHookOrder_Insert_AfterInsertError_StopsBeforeAfterSave(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()
	resetHookOrderCalls(t)

	err := den.Insert(ctx, db, &orderingDoc{Name: "x", FailHook: "AfterInsert"})
	require.Error(t, err)
	assert.Equal(t,
		[]string{"BeforeInsert", "BeforeSave", "Validate", "AfterInsert"},
		hookOrderCalls,
		"AfterInsert error must short-circuit the After-chain before AfterSave",
	)

	count, err := den.NewQuery[orderingDoc](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count,
		"the write happened before After-hooks ran; an AfterInsert error does not roll it back")
}

// --- Update ---

func TestHookOrder_Update_FullChain(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()

	doc := &orderingDoc{Name: "seed"}
	require.NoError(t, den.Insert(ctx, db, doc))
	resetHookOrderCalls(t) // discard insert-side hooks

	doc.Name = "updated"
	require.NoError(t, den.Update(ctx, db, doc))
	assert.Equal(t,
		[]string{"BeforeUpdate", "BeforeSave", "Validate", "AfterUpdate", "AfterSave"},
		hookOrderCalls)
}

func TestHookOrder_Update_ValidateError_StopsBeforeWrite(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()

	doc := &orderingDoc{Name: "seed"}
	require.NoError(t, den.Insert(ctx, db, doc))
	resetHookOrderCalls(t)

	doc.Name = "updated"
	doc.FailHook = "Validate"
	err := den.Update(ctx, db, doc)
	require.ErrorIs(t, err, den.ErrValidation)
	assert.Equal(t, []string{"BeforeUpdate", "BeforeSave", "Validate"}, hookOrderCalls)

	persisted, err := den.FindByID[orderingDoc](ctx, db, doc.ID)
	require.NoError(t, err)
	assert.Equal(t, "seed", persisted.Name, "Validate failure must not write the new value")
}

// --- Delete (hard) ---

func TestHookOrder_Delete_FullChain(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()

	doc := &orderingDoc{Name: "seed"}
	require.NoError(t, den.Insert(ctx, db, doc))
	resetHookOrderCalls(t)

	require.NoError(t, den.Delete(ctx, db, doc))
	assert.Equal(t, []string{"BeforeDelete", "AfterDelete"}, hookOrderCalls,
		"Delete fires no Saver hooks — only the dedicated Delete pair")
}

func TestHookOrder_Delete_BeforeError_StopsImmediately(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()

	doc := &orderingDoc{Name: "seed"}
	require.NoError(t, den.Insert(ctx, db, doc))
	resetHookOrderCalls(t)
	doc.FailHook = "BeforeDelete"

	err := den.Delete(ctx, db, doc)
	require.Error(t, err)
	assert.Equal(t, []string{"BeforeDelete"}, hookOrderCalls)

	persisted, err := den.FindByID[orderingDoc](ctx, db, doc.ID)
	require.NoError(t, err)
	assert.Equal(t, "seed", persisted.Name, "BeforeDelete failure must leave the document intact")
}

// --- Soft-Delete ---

func TestHookOrder_SoftDelete_FullChain(t *testing.T) {
	db := dentest.MustOpen(t, &orderingSoftDoc{})
	ctx := context.Background()

	doc := &orderingSoftDoc{Name: "seed"}
	require.NoError(t, den.Insert(ctx, db, doc))
	resetHookOrderCalls(t)

	require.NoError(t, den.Delete(ctx, db, doc))
	assert.Equal(t,
		[]string{"BeforeDelete", "BeforeSoftDelete", "AfterSoftDelete", "AfterDelete"},
		hookOrderCalls,
		"Soft-delete nests the soft-only hook pair inside the general Delete pair")
	assert.True(t, doc.IsDeleted())
}

func TestHookOrder_SoftDelete_BeforeError_StopsImmediately(t *testing.T) {
	db := dentest.MustOpen(t, &orderingSoftDoc{})
	ctx := context.Background()

	doc := &orderingSoftDoc{Name: "seed"}
	require.NoError(t, den.Insert(ctx, db, doc))
	resetHookOrderCalls(t)
	doc.FailHook = "BeforeDelete"

	err := den.Delete(ctx, db, doc)
	require.Error(t, err)
	assert.Equal(t, []string{"BeforeDelete"}, hookOrderCalls)

	persisted, err := den.FindByID[orderingSoftDoc](ctx, db, doc.ID)
	require.NoError(t, err)
	assert.False(t, persisted.IsDeleted(),
		"BeforeDelete failure must skip the soft-delete write")
}

func TestHookOrder_SoftDelete_BeforeSoftError_SkipsWrite(t *testing.T) {
	db := dentest.MustOpen(t, &orderingSoftDoc{})
	ctx := context.Background()

	doc := &orderingSoftDoc{Name: "seed"}
	require.NoError(t, den.Insert(ctx, db, doc))
	resetHookOrderCalls(t)
	doc.FailHook = "BeforeSoftDelete"

	err := den.Delete(ctx, db, doc)
	require.Error(t, err)
	assert.Equal(t, []string{"BeforeDelete", "BeforeSoftDelete"}, hookOrderCalls,
		"BeforeSoftDelete failure aborts before the write and before After hooks fire")

	persisted, err := den.FindByID[orderingSoftDoc](ctx, db, doc.ID)
	require.NoError(t, err)
	assert.False(t, persisted.IsDeleted(),
		"BeforeSoftDelete failure must leave DeletedAt unset")
}

// TestHookOrder_HardDeleteOfSoftDoc_SkipsSoftHooks pins that HardDelete() on
// a SoftDelete-embedding document fires only the general Delete hooks — the
// soft-only hooks must NOT fire because the soft-delete path is bypassed.
func TestHookOrder_HardDeleteOfSoftDoc_SkipsSoftHooks(t *testing.T) {
	db := dentest.MustOpen(t, &orderingSoftDoc{})
	ctx := context.Background()

	doc := &orderingSoftDoc{Name: "seed"}
	require.NoError(t, den.Insert(ctx, db, doc))
	resetHookOrderCalls(t)

	require.NoError(t, den.Delete(ctx, db, doc, den.HardDelete()))
	assert.Equal(t, []string{"BeforeDelete", "AfterDelete"}, hookOrderCalls,
		"HardDelete bypasses soft-delete and must not fire BeforeSoftDelete/AfterSoftDelete")

	_, err := den.FindByID[orderingSoftDoc](ctx, db, doc.ID)
	require.ErrorIs(t, err, den.ErrNotFound)
}

// --- InsertMany ---

func TestHookOrder_InsertMany_PerDoc(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()
	resetHookOrderCalls(t)

	docs := []*orderingDoc{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	require.NoError(t, den.InsertMany(ctx, db, docs))

	chain := []string{"BeforeInsert", "BeforeSave", "Validate", "AfterInsert", "AfterSave"}
	expected := slices.Repeat(chain, len(docs))
	assert.Equal(t, expected, hookOrderCalls,
		"InsertMany walks the full hook chain per document, in input order")
}

func TestHookOrder_InsertMany_FailMidBatch_RollsBack(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()

	require.NoError(t, den.Insert(ctx, db, &orderingDoc{Name: "preexisting"}))
	resetHookOrderCalls(t)

	// Doc 0 succeeds; doc 1 fails at Validate; doc 2 must never be touched.
	docs := []*orderingDoc{
		{Name: "0"},
		{Name: "fails", FailHook: "Validate"},
		{Name: "2"},
	}
	err := den.InsertMany(ctx, db, docs)
	require.ErrorIs(t, err, den.ErrValidation)

	expected := []string{
		// doc 0: full chain
		"BeforeInsert", "BeforeSave", "Validate", "AfterInsert", "AfterSave",
		// doc 1: stops at Validate
		"BeforeInsert", "BeforeSave", "Validate",
	}
	assert.Equal(t, expected, hookOrderCalls,
		"InsertMany must stop at the first failing doc and not enter doc 2's hooks")

	count, err := den.NewQuery[orderingDoc](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count,
		"the batch tx rolls back — only the pre-batch seed survives")
}
