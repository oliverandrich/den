package den_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/where"
)

type Product struct {
	document.Base
	Name  string  `json:"name" den:"index"`
	Price float64 `json:"price"`
}

func TestInsertAndFindByID(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 29.99}
	err := den.Insert(ctx, db, p)
	require.NoError(t, err)

	assert.NotEmpty(t, p.ID, "ID should be auto-generated")
	assert.NotZero(t, p.CreatedAt, "CreatedAt should be set")
	assert.NotZero(t, p.UpdatedAt, "UpdatedAt should be set")

	found, err := den.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.Equal(t, p.Name, found.Name)
	assert.InDelta(t, p.Price, found.Price, 0.001)
	assert.Equal(t, p.ID, found.ID)
}

func TestInsertWithCustomID(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{
		Base: document.Base{ID: "custom-123"},
		Name: "Custom",
	}
	require.NoError(t, den.Insert(ctx, db, p))

	assert.Equal(t, "custom-123", p.ID)

	found, err := den.FindByID[Product](ctx, db, "custom-123")
	require.NoError(t, err)
	assert.Equal(t, "Custom", found.Name)
}

func TestFindByID_NotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	_, err := den.FindByID[Product](ctx, db, "nonexistent")
	require.ErrorIs(t, err, den.ErrNotFound)
}

func TestFindByIDs(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p1 := &Product{Name: "A", Price: 1.0}
	p2 := &Product{Name: "B", Price: 2.0}
	p3 := &Product{Name: "C", Price: 3.0}
	require.NoError(t, den.InsertMany(ctx, db, []*Product{p1, p2, p3}))

	// Fetch two of three
	docs, err := den.FindByIDs[Product](ctx, db, []string{p1.ID, p3.ID})
	require.NoError(t, err)
	assert.Len(t, docs, 2)

	names := map[string]bool{}
	for _, d := range docs {
		names[d.Name] = true
	}
	assert.True(t, names["A"])
	assert.True(t, names["C"])
}

func TestFindByIDs_MissingIDs(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Only", Price: 1.0}
	require.NoError(t, den.Insert(ctx, db, p))

	// One real, one fake — should return only the real one
	docs, err := den.FindByIDs[Product](ctx, db, []string{p.ID, "nonexistent"})
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	assert.Equal(t, "Only", docs[0].Name)
}

func TestFindByIDs_Empty(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	docs, err := den.FindByIDs[Product](ctx, db, nil)
	require.NoError(t, err)
	assert.Empty(t, docs)
}

// TestUpdateMany_DelegatesToQuerySet pins that the top-level shim
// produces the same result as the chained QuerySet.Update form. It is
// pure ergonomics — the test just confirms the rows actually change.
func TestUpdateMany_DelegatesToQuerySet(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	require.NoError(t, den.InsertMany(ctx, db, []*Product{
		{Name: "A", Price: 1.0},
		{Name: "B", Price: 2.0},
		{Name: "C", Price: 99.0},
	}))

	count, err := den.UpdateMany[Product](ctx, db,
		[]where.Condition{where.Field("price").Lt(10.0)},
		den.SetFields{"price": 50.0},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	updated, err := den.NewQuery[Product](db, where.Field("price").Eq(50.0)).All(ctx)
	require.NoError(t, err)
	assert.Len(t, updated, 2)
}

// TestSave_InsertWhenIDEmpty pins that Save routes to Insert when the
// document has no ID yet — the new-document path. After Save returns the
// ID is populated, mirroring Insert's contract.
func TestSave_InsertWhenIDEmpty(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "FreshDoc", Price: 5.0}
	require.NoError(t, den.Save(ctx, db, p))
	assert.NotEmpty(t, p.ID, "Save must set the ID via the Insert path")

	found, err := den.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "FreshDoc", found.Name)
}

// TestSave_UpdateWhenIDPresent pins that Save routes to Update when the
// document already has an ID — the existing-document path. The stored
// row must reflect the in-memory state after the call.
func TestSave_UpdateWhenIDPresent(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Initial", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))
	originalID := p.ID

	p.Price = 99.0
	require.NoError(t, den.Save(ctx, db, p))
	assert.Equal(t, originalID, p.ID, "Update must not change the ID")

	found, err := den.FindByID[Product](ctx, db, originalID)
	require.NoError(t, err)
	assert.InDelta(t, 99.0, found.Price, 0.001,
		"Save must persist the in-memory state through the Update path")
}

func TestUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Original", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))
	originalUpdatedAt := p.UpdatedAt

	time.Sleep(2 * time.Millisecond)
	p.Name = "Updated"
	p.Price = 20.0
	require.NoError(t, den.Update(ctx, db, p))

	assert.True(t, p.UpdatedAt.After(originalUpdatedAt), "UpdatedAt should be bumped")

	found, err := den.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated", found.Name)
	assert.InDelta(t, 20.0, found.Price, 0.001)
}

func TestDelete(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "ToDelete"}
	require.NoError(t, den.Insert(ctx, db, p))

	require.NoError(t, den.Delete(ctx, db, p))

	_, err := den.FindByID[Product](ctx, db, p.ID)
	require.ErrorIs(t, err, den.ErrNotFound)
}

func TestInsert_Upsert(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "New"}
	require.NoError(t, den.Insert(ctx, db, p))

	assert.NotEmpty(t, p.ID)

	found, err := den.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "New", found.Name)
}

func TestUpdate_ViaSave(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Original"}
	require.NoError(t, den.Insert(ctx, db, p))

	p.Name = "Updated"
	require.NoError(t, den.Update(ctx, db, p))

	found, err := den.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated", found.Name)
}

func TestRefresh(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Original", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	// Simulate external change by directly updating
	p2 := &Product{
		Base:  document.Base{ID: p.ID, CreatedAt: p.CreatedAt},
		Name:  "Changed",
		Price: 99.0,
	}
	require.NoError(t, den.Update(ctx, db, p2))

	// p still has old values
	assert.Equal(t, "Original", p.Name)

	// Refresh picks up the change
	require.NoError(t, den.Refresh(ctx, db, p))
	assert.Equal(t, "Changed", p.Name)
	assert.InDelta(t, 99.0, p.Price, 0.001)
}

func TestRefresh_NotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "WillBeDeleted"}
	require.NoError(t, den.Insert(ctx, db, p))
	require.NoError(t, den.Delete(ctx, db, p))

	err := den.Refresh(ctx, db, p)
	require.ErrorIs(t, err, den.ErrNotFound)
}

func TestUnregisteredType(t *testing.T) {
	db := dentest.MustOpen(t) // no types registered
	ctx := context.Background()

	p := &Product{Name: "Orphan"}
	err := den.Insert(ctx, db, p)
	require.ErrorIs(t, err, den.ErrNotRegistered)
}

// --- Unique constraint tests ---

type UniqueProduct struct {
	document.Base
	Name string `json:"name"`
	SKU  string `json:"sku" den:"unique"`
}

type NullableUniqueUser struct {
	document.Base
	Username string  `json:"username" den:"unique"`
	Email    *string `json:"email,omitempty" den:"unique"`
}

func TestUniqueConstraint(t *testing.T) {
	db := dentest.MustOpen(t, &UniqueProduct{})
	ctx := context.Background()

	p1 := &UniqueProduct{Name: "Widget A", SKU: "ABC123"}
	require.NoError(t, den.Insert(ctx, db, p1))

	p2 := &UniqueProduct{Name: "Widget B", SKU: "ABC123"}
	err := den.Insert(ctx, db, p2)
	require.ErrorIs(t, err, den.ErrDuplicate)
}

func TestUniqueConstraint_DifferentValues(t *testing.T) {
	db := dentest.MustOpen(t, &UniqueProduct{})
	ctx := context.Background()

	p1 := &UniqueProduct{Name: "Widget A", SKU: "ABC123"}
	require.NoError(t, den.Insert(ctx, db, p1))

	p2 := &UniqueProduct{Name: "Widget B", SKU: "DEF456"}
	require.NoError(t, den.Insert(ctx, db, p2))
}

func TestUniqueConstraint_UpdateKeepsSameValue(t *testing.T) {
	db := dentest.MustOpen(t, &UniqueProduct{})
	ctx := context.Background()

	p := &UniqueProduct{Name: "Widget", SKU: "ABC123"}
	require.NoError(t, den.Insert(ctx, db, p))

	p.Name = "Updated Widget"
	require.NoError(t, den.Update(ctx, db, p))

	found, err := den.FindByID[UniqueProduct](ctx, db, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Widget", found.Name)
	assert.Equal(t, "ABC123", found.SKU)
}

func TestUniqueConstraint_UpdateChangesValue(t *testing.T) {
	db := dentest.MustOpen(t, &UniqueProduct{})
	ctx := context.Background()

	p := &UniqueProduct{Name: "Widget", SKU: "ABC123"}
	require.NoError(t, den.Insert(ctx, db, p))

	p.SKU = "NEW456"
	require.NoError(t, den.Update(ctx, db, p))

	// Old unique value should be freed
	p2 := &UniqueProduct{Name: "Other", SKU: "ABC123"}
	require.NoError(t, den.Insert(ctx, db, p2))
}

func TestUniqueConstraint_DeleteFreesValue(t *testing.T) {
	db := dentest.MustOpen(t, &UniqueProduct{})
	ctx := context.Background()

	p := &UniqueProduct{Name: "Widget", SKU: "ABC123"}
	require.NoError(t, den.Insert(ctx, db, p))
	require.NoError(t, den.Delete(ctx, db, p))

	// Unique value should be available again
	p2 := &UniqueProduct{Name: "New Widget", SKU: "ABC123"}
	require.NoError(t, den.Insert(ctx, db, p2))
}

func ptr(s string) *string { return &s }

func TestNullableUnique_MultipleNils(t *testing.T) {
	db := dentest.MustOpen(t, &NullableUniqueUser{})
	ctx := context.Background()

	u1 := &NullableUniqueUser{Username: "alice"}
	require.NoError(t, den.Insert(ctx, db, u1))

	u2 := &NullableUniqueUser{Username: "bob"}
	require.NoError(t, den.Insert(ctx, db, u2))
}

func TestNullableUnique_ConflictOnNonNil(t *testing.T) {
	db := dentest.MustOpen(t, &NullableUniqueUser{})
	ctx := context.Background()

	u1 := &NullableUniqueUser{Username: "alice", Email: ptr("alice@example.com")}
	require.NoError(t, den.Insert(ctx, db, u1))

	u2 := &NullableUniqueUser{Username: "bob", Email: ptr("alice@example.com")}
	err := den.Insert(ctx, db, u2)
	require.ErrorIs(t, err, den.ErrDuplicate)
}

func TestNullableUnique_DifferentNonNil(t *testing.T) {
	db := dentest.MustOpen(t, &NullableUniqueUser{})
	ctx := context.Background()

	u1 := &NullableUniqueUser{Username: "alice", Email: ptr("alice@example.com")}
	require.NoError(t, den.Insert(ctx, db, u1))

	u2 := &NullableUniqueUser{Username: "bob", Email: ptr("bob@example.com")}
	require.NoError(t, den.Insert(ctx, db, u2))
}

// --- Bulk operation tests ---

func TestInsertMany(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	products := []*Product{
		{Name: "A", Price: 1.0},
		{Name: "B", Price: 2.0},
		{Name: "C", Price: 3.0},
	}
	require.NoError(t, den.InsertMany(ctx, db, products))

	for _, p := range products {
		assert.NotEmpty(t, p.ID)
	}

	all, err := den.NewQuery[Product](db).All(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

// counterHookCalls records hook invocations on counterHookDoc to pin the
// "PreValidate fires every hook twice" contract. Tests touching it must
// NOT use t.Parallel — it has no synchronization.
var counterHookCalls []string

type counterHookDoc struct {
	document.Base
	Name string `json:"name"`
}

func (c *counterHookDoc) BeforeInsert(_ context.Context) error {
	counterHookCalls = append(counterHookCalls, "BeforeInsert")
	return nil
}

func (c *counterHookDoc) BeforeSave(_ context.Context) error {
	counterHookCalls = append(counterHookCalls, "BeforeSave")
	return nil
}

func (c *counterHookDoc) Validate(_ context.Context) error {
	counterHookCalls = append(counterHookCalls, "Validate")
	return nil
}

func TestInsertMany_PreValidate_AllValid(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	docs := []*Validated{{Name: "A"}, {Name: "B"}, {Name: "C"}}
	require.NoError(t, den.InsertMany(ctx, db, docs, den.PreValidate()))

	all, err := den.NewQuery[Validated](db).All(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestInsertMany_PreValidate_FailsAtEnd(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	docs := []*Validated{{Name: "A"}, {Name: "B"}, {Name: ""}}
	err := den.InsertMany(ctx, db, docs, den.PreValidate())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "index 2", "error must point at the failing document")
	require.ErrorIs(t, err, den.ErrValidation)

	count, err := den.NewQuery[Validated](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count, "no document is written when pre-validation fails")
}

func TestInsertMany_PreValidate_FailsAtStart(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	docs := []*Validated{{Name: ""}, {Name: "B"}, {Name: "C"}}
	err := den.InsertMany(ctx, db, docs, den.PreValidate())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "index 0")
	require.ErrorIs(t, err, den.ErrValidation)

	count, err := den.NewQuery[Validated](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

// counterLinkedDoc is counterHookDoc + a Link field so the LinkWrite path
// can be exercised while the same counterHookCalls recorder is reused.
type counterLinkedDoc struct {
	document.Base
	Name string         `json:"name"`
	Ref  den.Link[Door] `json:"ref"`
}

func (d *counterLinkedDoc) BeforeInsert(_ context.Context) error {
	counterHookCalls = append(counterHookCalls, "BeforeInsert")
	return nil
}

func (d *counterLinkedDoc) BeforeSave(_ context.Context) error {
	counterHookCalls = append(counterHookCalls, "BeforeSave")
	return nil
}

func (d *counterLinkedDoc) Validate(_ context.Context) error {
	counterHookCalls = append(counterHookCalls, "Validate")
	return nil
}

func TestInsertMany_PreValidate_LinkWrite_HooksRunTwice(t *testing.T) {
	// Pins the documented exception: PreValidate + LinkWrite disables the
	// caching optimization, so the prep chain runs once outside the tx and
	// again inside it.
	db := dentest.MustOpen(t, &Door{}, &counterLinkedDoc{})
	ctx := context.Background()
	counterHookCalls = nil
	t.Cleanup(func() { counterHookCalls = nil })

	docs := []*counterLinkedDoc{{
		Name: "A",
		Ref:  den.NewLink(&Door{Height: 200, Width: 80}),
	}}
	require.NoError(t, den.InsertMany(ctx, db, docs,
		den.PreValidate(), den.WithLinkRule(den.LinkWrite)))

	expected := []string{
		"BeforeInsert", "BeforeSave", "Validate",
		"BeforeInsert", "BeforeSave", "Validate",
	}
	assert.Equal(t, expected, counterHookCalls)
}

func TestInsertMany_PreValidate_LinkWrite_CascadesAndPersists(t *testing.T) {
	// Guards the LinkWrite + PreValidate branch: cascade must still happen
	// (the optimization doesn't apply; the fallback runs the standard Insert
	// per-doc inside the tx). The main contract tested here is that children
	// get written — not the hook count.
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	ctx := context.Background()

	door1 := &Door{Height: 200, Width: 80}
	door2 := &Door{Height: 210, Width: 90}
	houses := []*House{
		{Name: "A", Door: den.NewLink(door1)},
		{Name: "B", Door: den.NewLink(door2)},
	}
	require.NoError(t, den.InsertMany(ctx, db, houses,
		den.PreValidate(), den.WithLinkRule(den.LinkWrite)))

	assert.NotEmpty(t, door1.ID)
	assert.NotEmpty(t, door2.ID)
	assert.NotEqual(t, door1.ID, door2.ID)

	for _, d := range []*Door{door1, door2} {
		found, err := den.FindByID[Door](ctx, db, d.ID)
		require.NoError(t, err)
		assert.Equal(t, d.Height, found.Height)
	}
}

func TestInsertMany_PreValidate_HooksRunOnce(t *testing.T) {
	db := dentest.MustOpen(t, &counterHookDoc{})
	ctx := context.Background()
	counterHookCalls = nil
	t.Cleanup(func() { counterHookCalls = nil })

	docs := []*counterHookDoc{{Name: "A"}}
	require.NoError(t, den.InsertMany(ctx, db, docs, den.PreValidate()))

	// Single pass: hooks run in the PreValidate pre-pass, then commitInsert
	// re-uses the encoded bytes — no second run.
	expected := []string{"BeforeInsert", "BeforeSave", "Validate"}
	assert.Equal(t, expected, counterHookCalls)
}

func TestInsertMany_ContinueOnError_PartialSuccess(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	docs := []*Validated{
		{Name: "A"},
		{Name: ""}, // invalid
		{Name: "C"},
		{Name: ""}, // invalid
		{Name: "E"},
	}
	err := den.InsertMany(ctx, db, docs, den.ContinueOnError())
	require.Error(t, err)

	var multi *den.InsertManyError
	require.ErrorAs(t, err, &multi)
	require.Len(t, multi.Failures, 2)
	assert.Equal(t, 1, multi.Failures[0].Index)
	assert.Equal(t, 3, multi.Failures[1].Index)
	require.ErrorIs(t, multi.Failures[0].Err, den.ErrValidation)
	require.ErrorIs(t, err, den.ErrValidation, "InsertManyError unwraps to wrapped sentinels")

	all, err := den.NewQuery[Validated](db).All(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3, "valid docs are written")
}

func TestInsertMany_ContinueOnError_AllSucceed(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	docs := []*Validated{{Name: "A"}, {Name: "B"}}
	require.NoError(t, den.InsertMany(ctx, db, docs, den.ContinueOnError()))

	count, err := den.NewQuery[Validated](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestInsertMany_ContinueOnError_AllFail(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	docs := []*Validated{{Name: ""}, {Name: ""}}
	err := den.InsertMany(ctx, db, docs, den.ContinueOnError())

	var multi *den.InsertManyError
	require.ErrorAs(t, err, &multi)
	assert.Len(t, multi.Failures, 2)

	count, err := den.NewQuery[Validated](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestInsertMany_ContinueOnError_RejectsTxScope(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		return den.InsertMany(ctx, tx, []*Validated{{Name: "A"}}, den.ContinueOnError())
	})
	require.ErrorIs(t, err, den.ErrIncompatibleScope)
}

func TestInsertMany_RejectsPreValidatePlusContinueOnError(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	err := den.InsertMany(ctx, db,
		[]*Validated{{Name: "A"}},
		den.PreValidate(), den.ContinueOnError(),
	)
	require.ErrorIs(t, err, den.ErrIncompatibleOptions)

	count, err := den.NewQuery[Validated](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count, "rejected option combo writes nothing")
}

func TestInsertMany_ContinueOnError_HonorsContextCancellation(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := den.InsertMany(ctx, db,
		[]*Validated{{Name: "A"}, {Name: "B"}},
		den.ContinueOnError(),
	)
	require.ErrorIs(t, err, context.Canceled)
}

func TestInsertMany_MaxRecordedFailures_CapsAt(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	docs := make([]*Validated, 20)
	for i := range docs {
		docs[i] = &Validated{Name: ""} // all invalid
	}

	err := den.InsertMany(ctx, db, docs,
		den.ContinueOnError(),
		den.MaxRecordedFailures(5),
	)
	var multi *den.InsertManyError
	require.ErrorAs(t, err, &multi)
	assert.Len(t, multi.Failures, 5, "failures slice is capped")
	assert.True(t, multi.Truncated, "Truncated reports the cap was hit")
	assert.Equal(t, 20, multi.TotalFailures, "TotalFailures reports the uncapped count")
}

func TestInsertMany_MaxRecordedFailures_UnderLimit_NotTruncated(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	docs := []*Validated{{Name: ""}, {Name: ""}, {Name: ""}}
	err := den.InsertMany(ctx, db, docs,
		den.ContinueOnError(),
		den.MaxRecordedFailures(10),
	)
	var multi *den.InsertManyError
	require.ErrorAs(t, err, &multi)
	assert.Len(t, multi.Failures, 3)
	assert.False(t, multi.Truncated)
	assert.Equal(t, 3, multi.TotalFailures)
}

func TestInsertMany_MaxRecordedFailures_Zero_Unlimited(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	// The default cap is 100 — confirm MaxRecordedFailures(0) disables it by
	// producing a batch larger than the default.
	docs := make([]*Validated, 150)
	for i := range docs {
		docs[i] = &Validated{Name: ""}
	}

	err := den.InsertMany(ctx, db, docs,
		den.ContinueOnError(),
		den.MaxRecordedFailures(0),
	)
	var multi *den.InsertManyError
	require.ErrorAs(t, err, &multi)
	assert.Len(t, multi.Failures, 150, "0 cap means unlimited")
	assert.False(t, multi.Truncated)
	assert.Equal(t, 150, multi.TotalFailures)
}

func TestInsertMany_DefaultRecordedFailuresCap(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	// With no MaxRecordedFailures option, the default cap of 100 kicks in.
	docs := make([]*Validated, 150)
	for i := range docs {
		docs[i] = &Validated{Name: ""}
	}

	err := den.InsertMany(ctx, db, docs, den.ContinueOnError())
	var multi *den.InsertManyError
	require.ErrorAs(t, err, &multi)
	assert.Len(t, multi.Failures, 100)
	assert.True(t, multi.Truncated)
	assert.Equal(t, 150, multi.TotalFailures)
}

func TestInsertMany_RejectsMaxRecordedFailuresWithoutContinueOnError(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	err := den.InsertMany(ctx, db,
		[]*Validated{{Name: "A"}},
		den.MaxRecordedFailures(10),
	)
	require.ErrorIs(t, err, den.ErrIncompatibleOptions,
		"MaxRecordedFailures is only meaningful with ContinueOnError")
}

func TestDeleteMany(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	products := []*Product{
		{Name: "Keep", Price: 5.0},
		{Name: "Delete1", Price: 15.0},
		{Name: "Delete2", Price: 25.0},
	}
	require.NoError(t, den.InsertMany(ctx, db, products))

	count, err := den.DeleteMany[Product](ctx, db, []where.Condition{where.Field("price").Gt(10.0)})
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	remaining, err := den.NewQuery[Product](db).All(ctx)
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, "Keep", remaining[0].Name)
}

// --- FindOneAndUpdate tests ---

func TestFindOneAndUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	updated, err := den.FindOneAndUpdate[Product](ctx, db,
		den.SetFields{"price": 99.0},
		[]where.Condition{where.Field("name").Eq("Widget")},
	)
	require.NoError(t, err)
	assert.InDelta(t, 99.0, updated.Price, 0.001)
	assert.Equal(t, "Widget", updated.Name)

	// Verify persisted
	found, err := den.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.InDelta(t, 99.0, found.Price, 0.001)
}

func TestFindOneAndUpdate_NotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	_, err := den.FindOneAndUpdate[Product](ctx, db,
		den.SetFields{"price": 99.0},
		[]where.Condition{where.Field("name").Eq("Nonexistent")},
	)
	require.ErrorIs(t, err, den.ErrNotFound)
}

func TestFindOneAndUpdate_FieldNotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	_, err := den.FindOneAndUpdate[Product](ctx, db,
		den.SetFields{"nonexistent": "x"},
		[]where.Condition{where.Field("name").Eq("Widget")},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field")
}

// TestFindOneAndUpdate_FieldValidatedBeforeLookup pins that a bad field name
// is caught before the find-and-update transaction opens, mirroring
// QuerySet.Update. Against an empty collection the in-tx applySetFields
// would never run because findOneStrict returns ErrNotFound first — only a
// pre-tx validation can surface the field-not-found error here.
func TestFindOneAndUpdate_FieldValidatedBeforeLookup(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	_, err := den.FindOneAndUpdate[Product](ctx, db,
		den.SetFields{"nonexistent": "x"},
		[]where.Condition{where.Field("name").Eq("absent")},
	)
	require.Error(t, err)
	require.NotErrorIs(t, err, den.ErrNotFound,
		"field validation must surface before findOneStrict runs")
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestFindOneAndUpdate_MultipleMatches(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	require.NoError(t, den.Insert(ctx, db, &Product{Name: "Widget", Price: 10.0}))
	require.NoError(t, den.Insert(ctx, db, &Product{Name: "Widget", Price: 20.0}))

	_, err := den.FindOneAndUpdate[Product](ctx, db,
		den.SetFields{"price": 99.0},
		[]where.Condition{where.Field("name").Eq("Widget")},
	)
	require.ErrorIs(t, err, den.ErrMultipleMatches)

	all, err := den.NewQuery[Product](db).Sort("price", den.Asc).All(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.InDelta(t, 10.0, all[0].Price, 0.001)
	assert.InDelta(t, 20.0, all[1].Price, 0.001)
}

// --- FindOrCreate tests (find-or-create-with-defaults shorthand) ---

// TestFindOrCreate_Insert pins the miss path: no row matches, defaults
// becomes the new document, inserted=true.
func TestFindOrCreate_Insert(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	doc, inserted, err := den.FindOrCreate[Product](ctx, db,
		&Product{Name: "Widget", Price: 1.0},
		[]where.Condition{where.Field("name").Eq("Widget")},
	)
	require.NoError(t, err)
	assert.True(t, inserted, "no existing match → must insert")
	assert.Equal(t, "Widget", doc.Name)
	assert.InDelta(t, 1.0, doc.Price, 0.001, "defaults price persisted as-is on insert")
	assert.NotEmpty(t, doc.ID)
}

// TestFindOrCreate_Existing pins the hit path: a matching row exists,
// it is returned untouched and inserted=false. The key contract that
// distinguishes FindOrCreate from FindOneAndUpsert: existing rows are
// NEVER modified.
func TestFindOrCreate_Existing(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	existing := &Product{Name: "Widget", Price: 99.0}
	require.NoError(t, den.Insert(ctx, db, existing))

	doc, inserted, err := den.FindOrCreate[Product](ctx, db,
		&Product{Name: "Widget", Price: 1.0}, // defaults — must NOT overwrite the 99.0
		[]where.Condition{where.Field("name").Eq("Widget")},
	)
	require.NoError(t, err)
	assert.False(t, inserted, "existing row → must not insert")
	assert.Equal(t, existing.ID, doc.ID, "must return the existing row's ID")
	assert.InDelta(t, 99.0, doc.Price, 0.001,
		"existing row price must be untouched — defaults apply only on miss")
}

// --- FindOneAndUpsert tests ---

func TestFindOneAndUpsert_Insert(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	doc, inserted, err := den.FindOneAndUpsert[Product](ctx, db,
		&Product{Name: "Widget", Price: 1.0},
		den.SetFields{"price": 5.0},
		[]where.Condition{where.Field("name").Eq("Widget")},
	)
	require.NoError(t, err)
	assert.True(t, inserted)
	assert.Equal(t, "Widget", doc.Name)
	assert.InDelta(t, 5.0, doc.Price, 0.001)
	assert.NotEmpty(t, doc.ID)

	found, err := den.FindByID[Product](ctx, db, doc.ID)
	require.NoError(t, err)
	assert.InDelta(t, 5.0, found.Price, 0.001)
}

func TestFindOneAndUpsert_Update(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	existing := &Product{Name: "Widget", Price: 1.0}
	require.NoError(t, den.Insert(ctx, db, existing))

	doc, inserted, err := den.FindOneAndUpsert[Product](ctx, db,
		&Product{Name: "Widget", Price: 999.0}, // defaults must NOT apply on hit
		den.SetFields{"price": 5.0},
		[]where.Condition{where.Field("name").Eq("Widget")},
	)
	require.NoError(t, err)
	assert.False(t, inserted)
	assert.Equal(t, existing.ID, doc.ID)
	assert.InDelta(t, 5.0, doc.Price, 0.001)

	count, err := den.NewQuery[Product](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestFindOneAndUpsert_MultipleMatches(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	require.NoError(t, den.Insert(ctx, db, &Product{Name: "Widget", Price: 1.0}))
	require.NoError(t, den.Insert(ctx, db, &Product{Name: "Widget", Price: 2.0}))

	_, _, err := den.FindOneAndUpsert[Product](ctx, db,
		&Product{Name: "Widget"},
		den.SetFields{"price": 99.0},
		[]where.Condition{where.Field("name").Eq("Widget")},
	)
	require.ErrorIs(t, err, den.ErrMultipleMatches)
}

func TestFindOneAndUpsert_FieldNotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	_, _, err := den.FindOneAndUpsert[Product](ctx, db,
		&Product{Name: "Widget"},
		den.SetFields{"nonexistent": "x"},
		[]where.Condition{where.Field("name").Eq("Widget")},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field")
}

func TestFindOneAndUpsert_SoftDeletedSkippedByDefault(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	original := &SoftProduct{Name: "Widget", Price: 1.0}
	require.NoError(t, den.Insert(ctx, db, original))
	require.NoError(t, den.Delete(ctx, db, original))

	doc, inserted, err := den.FindOneAndUpsert[SoftProduct](ctx, db,
		&SoftProduct{Name: "Widget", Price: 10.0},
		den.SetFields{"price": 20.0},
		[]where.Condition{where.Field("name").Eq("Widget")},
	)
	require.NoError(t, err)
	assert.True(t, inserted, "soft-deleted match should not satisfy upsert")
	assert.NotEqual(t, original.ID, doc.ID)
	assert.InDelta(t, 20.0, doc.Price, 0.001)
}

func TestFindOneAndUpsert_IncludeDeletedUpdates(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	original := &SoftProduct{Name: "Widget", Price: 1.0}
	require.NoError(t, den.Insert(ctx, db, original))
	require.NoError(t, den.Delete(ctx, db, original))

	doc, inserted, err := den.FindOneAndUpsert[SoftProduct](ctx, db,
		&SoftProduct{Name: "Widget", Price: 10.0},
		den.SetFields{"price": 20.0},
		[]where.Condition{where.Field("name").Eq("Widget")},
		den.IncludeDeleted(),
	)
	require.NoError(t, err)
	assert.False(t, inserted, "soft-deleted match should be updated")
	assert.Equal(t, original.ID, doc.ID)
	assert.InDelta(t, 20.0, doc.Price, 0.001)
	assert.True(t, doc.IsDeleted(), "DeletedAt is preserved; caller clears it via SetFields if desired")
}

func TestFindOneAndUpsert_HookOrder_InsertPath(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()
	resetHookOrderCalls(t)

	_, inserted, err := den.FindOneAndUpsert[orderingDoc](ctx, db,
		&orderingDoc{Name: "Widget"},
		den.SetFields{},
		[]where.Condition{where.Field("name").Eq("Widget")},
	)
	require.NoError(t, err)
	assert.True(t, inserted)
	assert.Equal(t,
		[]string{"BeforeInsert", "BeforeSave", "Validate", "AfterInsert", "AfterSave"},
		hookOrderCalls,
		"only Insert hooks fire on miss path",
	)
}

func TestFindOneAndUpsert_HookOrder_UpdatePath(t *testing.T) {
	db := dentest.MustOpen(t, &orderingDoc{})
	ctx := context.Background()

	require.NoError(t, den.Insert(ctx, db, &orderingDoc{Name: "Widget"}))
	resetHookOrderCalls(t) // discard insert hooks from seed

	_, inserted, err := den.FindOneAndUpsert[orderingDoc](ctx, db,
		&orderingDoc{Name: "Widget"},
		den.SetFields{},
		[]where.Condition{where.Field("name").Eq("Widget")},
	)
	require.NoError(t, err)
	assert.False(t, inserted)
	assert.Equal(t,
		[]string{"BeforeUpdate", "BeforeSave", "Validate", "AfterUpdate", "AfterSave"},
		hookOrderCalls,
		"only Update hooks fire on hit path",
	)
}

func TestUpdate_MissingIDWrapsErrValidation(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	err := den.Update(ctx, db, &Product{Name: "NoID"})
	require.Error(t, err)
	require.ErrorIs(t, err, den.ErrValidation)
}

func TestDelete_MissingIDWrapsErrValidation(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	err := den.Delete(ctx, db, &Product{Name: "NoID"})
	require.Error(t, err)
	require.ErrorIs(t, err, den.ErrValidation)
}

// --- den:"eager" honored across CRUD-style read APIs ---

// seedEagerHouse inserts one Door + one EagerHouse pointing at it,
// returning the persisted house. Used by the eager-CRUD tests below.
// EagerHouse / Door / EagerOwner are defined in link_test.go.
func seedEagerHouse(ctx context.Context, t *testing.T, db *den.DB, name string) *EagerHouse {
	t.Helper()
	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))
	owner := &EagerOwner{Name: "Owner"}
	require.NoError(t, den.Insert(ctx, db, owner))
	h := &EagerHouse{Name: name, Door: den.NewLink(door), Owner: den.NewLink(owner)}
	require.NoError(t, den.Insert(ctx, db, h))
	return h
}

func TestFindByID_HonorsEagerTag(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	h := seedEagerHouse(ctx, t, db, "Cottage")

	got, err := den.FindByID[EagerHouse](ctx, db, h.ID)
	require.NoError(t, err)
	assert.True(t, got.Door.IsLoaded(), "FindByID must honor den:\"eager\" on Door")
	assert.False(t, got.Owner.IsLoaded(), "untagged Owner stays lazy")
}

func TestFindByID_WithoutFetchLinksSuppressesEager(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	h := seedEagerHouse(ctx, t, db, "Cottage")

	got, err := den.FindByID[EagerHouse](ctx, db, h.ID, den.WithoutFetchLinks())
	require.NoError(t, err)
	assert.False(t, got.Door.IsLoaded(), "WithoutFetchLinks must override eager tag")
	assert.NotEmpty(t, got.Door.ID, "ID still populated")
}

func TestFindByIDs_HonorsEagerTag(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	h1 := seedEagerHouse(ctx, t, db, "A")
	h2 := seedEagerHouse(ctx, t, db, "B")

	got, err := den.FindByIDs[EagerHouse](ctx, db, []string{h1.ID, h2.ID})
	require.NoError(t, err)
	require.Len(t, got, 2)
	for _, h := range got {
		assert.True(t, h.Door.IsLoaded(), "FindByIDs must honor eager tag for every result")
	}
}

func TestRefresh_HonorsEagerTag(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	h := seedEagerHouse(ctx, t, db, "Cottage")
	stale := &EagerHouse{Base: document.Base{ID: h.ID}}

	require.NoError(t, den.Refresh(ctx, db, stale))
	assert.True(t, stale.Door.IsLoaded(), "Refresh must honor den:\"eager\"")
}

func TestFindOneAndUpdate_HonorsEagerTag(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	h := seedEagerHouse(ctx, t, db, "Cottage")

	got, err := den.FindOneAndUpdate[EagerHouse](ctx, db,
		den.SetFields{"name": "Renamed"},
		[]where.Condition{where.Field("_id").Eq(h.ID)},
	)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", got.Name)
	assert.True(t, got.Door.IsLoaded(), "FindOneAndUpdate must honor den:\"eager\"")
}

func TestFindOneAndUpsert_HonorsEagerTag_HitPath(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	h := seedEagerHouse(ctx, t, db, "Cottage")

	got, inserted, err := den.FindOneAndUpsert[EagerHouse](ctx, db,
		&EagerHouse{Name: "should-not-insert"},
		den.SetFields{"name": "Updated"},
		[]where.Condition{where.Field("_id").Eq(h.ID)},
	)
	require.NoError(t, err)
	assert.False(t, inserted)
	assert.True(t, got.Door.IsLoaded(), "Upsert hit path must honor den:\"eager\"")
}

func TestFindOneAndUpsert_HonorsEagerTag_MissPath(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, den.Insert(ctx, db, door))

	defaults := &EagerHouse{Name: "Fresh", Door: den.NewLink(door)}
	got, inserted, err := den.FindOneAndUpsert[EagerHouse](ctx, db,
		defaults,
		den.SetFields{},
		[]where.Condition{where.Field("name").Eq("Fresh")},
	)
	require.NoError(t, err)
	assert.True(t, inserted)
	assert.True(t, got.Door.IsLoaded(),
		"Upsert miss path must hydrate eager links on the freshly inserted doc")
}

func TestFindOrCreate_HonorsEagerTag(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	h := seedEagerHouse(ctx, t, db, "Cottage")

	got, inserted, err := den.FindOrCreate[EagerHouse](ctx, db,
		&EagerHouse{Name: "should-not-insert"},
		[]where.Condition{where.Field("_id").Eq(h.ID)},
	)
	require.NoError(t, err)
	assert.False(t, inserted)
	assert.True(t, got.Door.IsLoaded(),
		"FindOrCreate delegates to upsert; must honor den:\"eager\"")
}
