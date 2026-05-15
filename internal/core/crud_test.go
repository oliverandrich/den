package core_test

import (
	"github.com/oliverandrich/den/internal/core"

	"context"
	"testing"
	"time"

	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/where"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	err := core.Save(ctx, db, p)
	require.NoError(t, err)

	assert.NotEmpty(t, p.ID, "ID should be auto-generated")
	assert.NotZero(t, p.CreatedAt, "CreatedAt should be set")
	assert.NotZero(t, p.UpdatedAt, "UpdatedAt should be set")

	found, err := core.FindByID[Product](ctx, db, p.ID)
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
	require.NoError(t, core.Save(ctx, db, p))

	assert.Equal(t, "custom-123", p.ID)

	found, err := core.FindByID[Product](ctx, db, "custom-123")
	require.NoError(t, err)
	assert.Equal(t, "Custom", found.Name)
}

func TestFindByID_NotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	_, err := core.FindByID[Product](ctx, db, "nonexistent")
	require.ErrorIs(t, err, core.ErrNotFound)
}

func TestFindByIDs(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p1 := &Product{Name: "A", Price: 1.0}
	p2 := &Product{Name: "B", Price: 2.0}
	p3 := &Product{Name: "C", Price: 3.0}
	require.NoError(t, core.SaveAll(ctx, db, []*Product{p1, p2, p3}))

	// Fetch two of three
	docs, err := core.FindByIDs[Product](ctx, db, []string{p1.ID, p3.ID})
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
	require.NoError(t, core.Save(ctx, db, p))

	// One real, one fake — should return only the real one
	docs, err := core.FindByIDs[Product](ctx, db, []string{p.ID, "nonexistent"})
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	assert.Equal(t, "Only", docs[0].Name)
}

func TestFindByIDs_Empty(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	docs, err := core.FindByIDs[Product](ctx, db, nil)
	require.NoError(t, err)
	assert.Empty(t, docs)
}

// TestQuerySet_Update_BulkPriceUpdate pins basic chained bulk update.
func TestQuerySet_Update_BulkPriceUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	require.NoError(t, core.SaveAll(ctx, db, []*Product{
		{Name: "A", Price: 1.0},
		{Name: "B", Price: 2.0},
		{Name: "C", Price: 99.0},
	}))

	count, err := core.NewQuery[Product](db, where.Field("price").Lt(10.0)).
		Update(ctx, core.SetFields{"price": 50.0})
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	updated, err := core.NewQuery[Product](db, where.Field("price").Eq(50.0)).All(ctx)
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
	require.NoError(t, core.Save(ctx, db, p))
	assert.NotEmpty(t, p.ID, "Save must set the ID via the Insert path")

	found, err := core.FindByID[Product](ctx, db, p.ID)
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
	require.NoError(t, core.Save(ctx, db, p))
	originalID := p.ID

	p.Price = 99.0
	require.NoError(t, core.Save(ctx, db, p))
	assert.Equal(t, originalID, p.ID, "Update must not change the ID")

	found, err := core.FindByID[Product](ctx, db, originalID)
	require.NoError(t, err)
	assert.InDelta(t, 99.0, found.Price, 0.001,
		"Save must persist the in-memory state through the Update path")
}

func TestUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Original", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))
	originalUpdatedAt := p.UpdatedAt

	time.Sleep(2 * time.Millisecond)
	p.Name = "Updated"
	p.Price = 20.0
	require.NoError(t, core.Save(ctx, db, p))

	assert.True(t, p.UpdatedAt.After(originalUpdatedAt), "UpdatedAt should be bumped")

	found, err := core.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated", found.Name)
	assert.InDelta(t, 20.0, found.Price, 0.001)
}

func TestDelete(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "ToDelete"}
	require.NoError(t, core.Save(ctx, db, p))

	require.NoError(t, core.Delete(ctx, db, p))

	_, err := core.FindByID[Product](ctx, db, p.ID)
	require.ErrorIs(t, err, core.ErrNotFound)
}

func TestInsert_Upsert(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "New"}
	require.NoError(t, core.Save(ctx, db, p))

	assert.NotEmpty(t, p.ID)

	found, err := core.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "New", found.Name)
}

func TestUpdate_ViaSave(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Original"}
	require.NoError(t, core.Save(ctx, db, p))

	p.Name = "Updated"
	require.NoError(t, core.Save(ctx, db, p))

	found, err := core.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated", found.Name)
}

func TestRefresh(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Original", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))

	// Simulate external change by directly updating
	p2 := &Product{
		Base:  document.Base{ID: p.ID, CreatedAt: p.CreatedAt},
		Name:  "Changed",
		Price: 99.0,
	}
	require.NoError(t, core.Save(ctx, db, p2))

	// p still has old values
	assert.Equal(t, "Original", p.Name)

	// Refresh picks up the change
	require.NoError(t, core.Refresh(ctx, db, p))
	assert.Equal(t, "Changed", p.Name)
	assert.InDelta(t, 99.0, p.Price, 0.001)
}

func TestRefresh_NotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "WillBeDeleted"}
	require.NoError(t, core.Save(ctx, db, p))
	require.NoError(t, core.Delete(ctx, db, p))

	err := core.Refresh(ctx, db, p)
	require.ErrorIs(t, err, core.ErrNotFound)
}

func TestUnregisteredType(t *testing.T) {
	db := dentest.MustOpen(t) // no types registered
	ctx := context.Background()

	p := &Product{Name: "Orphan"}
	err := core.Save(ctx, db, p)
	require.ErrorIs(t, err, core.ErrNotRegistered)
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
	require.NoError(t, core.Save(ctx, db, p1))

	p2 := &UniqueProduct{Name: "Widget B", SKU: "ABC123"}
	err := core.Save(ctx, db, p2)
	require.ErrorIs(t, err, core.ErrDuplicate)
}

func TestUniqueConstraint_DifferentValues(t *testing.T) {
	db := dentest.MustOpen(t, &UniqueProduct{})
	ctx := context.Background()

	p1 := &UniqueProduct{Name: "Widget A", SKU: "ABC123"}
	require.NoError(t, core.Save(ctx, db, p1))

	p2 := &UniqueProduct{Name: "Widget B", SKU: "DEF456"}
	require.NoError(t, core.Save(ctx, db, p2))
}

func TestUniqueConstraint_UpdateKeepsSameValue(t *testing.T) {
	db := dentest.MustOpen(t, &UniqueProduct{})
	ctx := context.Background()

	p := &UniqueProduct{Name: "Widget", SKU: "ABC123"}
	require.NoError(t, core.Save(ctx, db, p))

	p.Name = "Updated Widget"
	require.NoError(t, core.Save(ctx, db, p))

	found, err := core.FindByID[UniqueProduct](ctx, db, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Widget", found.Name)
	assert.Equal(t, "ABC123", found.SKU)
}

func TestUniqueConstraint_UpdateChangesValue(t *testing.T) {
	db := dentest.MustOpen(t, &UniqueProduct{})
	ctx := context.Background()

	p := &UniqueProduct{Name: "Widget", SKU: "ABC123"}
	require.NoError(t, core.Save(ctx, db, p))

	p.SKU = "NEW456"
	require.NoError(t, core.Save(ctx, db, p))

	// Old unique value should be freed
	p2 := &UniqueProduct{Name: "Other", SKU: "ABC123"}
	require.NoError(t, core.Save(ctx, db, p2))
}

func TestUniqueConstraint_DeleteFreesValue(t *testing.T) {
	db := dentest.MustOpen(t, &UniqueProduct{})
	ctx := context.Background()

	p := &UniqueProduct{Name: "Widget", SKU: "ABC123"}
	require.NoError(t, core.Save(ctx, db, p))
	require.NoError(t, core.Delete(ctx, db, p))

	// Unique value should be available again
	p2 := &UniqueProduct{Name: "New Widget", SKU: "ABC123"}
	require.NoError(t, core.Save(ctx, db, p2))
}

func ptr(s string) *string { return &s }

func TestNullableUnique_MultipleNils(t *testing.T) {
	db := dentest.MustOpen(t, &NullableUniqueUser{})
	ctx := context.Background()

	u1 := &NullableUniqueUser{Username: "alice"}
	require.NoError(t, core.Save(ctx, db, u1))

	u2 := &NullableUniqueUser{Username: "bob"}
	require.NoError(t, core.Save(ctx, db, u2))
}

func TestNullableUnique_ConflictOnNonNil(t *testing.T) {
	db := dentest.MustOpen(t, &NullableUniqueUser{})
	ctx := context.Background()

	u1 := &NullableUniqueUser{Username: "alice", Email: ptr("alice@example.com")}
	require.NoError(t, core.Save(ctx, db, u1))

	u2 := &NullableUniqueUser{Username: "bob", Email: ptr("alice@example.com")}
	err := core.Save(ctx, db, u2)
	require.ErrorIs(t, err, core.ErrDuplicate)
}

func TestNullableUnique_DifferentNonNil(t *testing.T) {
	db := dentest.MustOpen(t, &NullableUniqueUser{})
	ctx := context.Background()

	u1 := &NullableUniqueUser{Username: "alice", Email: ptr("alice@example.com")}
	require.NoError(t, core.Save(ctx, db, u1))

	u2 := &NullableUniqueUser{Username: "bob", Email: ptr("bob@example.com")}
	require.NoError(t, core.Save(ctx, db, u2))
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
	require.NoError(t, core.SaveAll(ctx, db, products))

	for _, p := range products {
		assert.NotEmpty(t, p.ID)
	}

	all, err := core.NewQuery[Product](db).All(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestDeleteMany(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	products := []*Product{
		{Name: "Keep", Price: 5.0},
		{Name: "Delete1", Price: 15.0},
		{Name: "Delete2", Price: 25.0},
	}
	require.NoError(t, core.SaveAll(ctx, db, products))

	count, err := core.NewQuery[Product](db, where.Field("price").Gt(10.0)).Delete(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	remaining, err := core.NewQuery[Product](db).All(ctx)
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, "Keep", remaining[0].Name)
}

// --- FindOneAndUpdate tests ---

func TestFindOneAndUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))

	updated, err := core.NewQuery[Product](db, where.Field("name").Eq("Widget")).UpdateOne(ctx, core.SetFields{"price": 99.0})
	require.NoError(t, err)
	assert.InDelta(t, 99.0, updated.Price, 0.001)
	assert.Equal(t, "Widget", updated.Name)

	// Verify persisted
	found, err := core.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.InDelta(t, 99.0, found.Price, 0.001)
}

func TestFindOneAndUpdate_NotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	_, err := core.NewQuery[Product](db, where.Field("name").Eq("Nonexistent")).UpdateOne(ctx, core.SetFields{"price": 99.0})
	require.ErrorIs(t, err, core.ErrNotFound)
}

func TestFindOneAndUpdate_FieldNotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 10.0}
	require.NoError(t, core.Save(ctx, db, p))

	_, err := core.NewQuery[Product](db, where.Field("name").Eq("Widget")).UpdateOne(ctx, core.SetFields{"nonexistent": "x"})
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

	_, err := core.NewQuery[Product](db, where.Field("name").Eq("absent")).UpdateOne(ctx, core.SetFields{"nonexistent": "x"})
	require.Error(t, err)
	require.NotErrorIs(t, err, core.ErrNotFound,
		"field validation must surface before findOneStrict runs")
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestFindOneAndUpdate_MultipleMatches(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	require.NoError(t, core.Save(ctx, db, &Product{Name: "Widget", Price: 10.0}))
	require.NoError(t, core.Save(ctx, db, &Product{Name: "Widget", Price: 20.0}))

	_, err := core.NewQuery[Product](db, where.Field("name").Eq("Widget")).UpdateOne(ctx, core.SetFields{"price": 99.0})
	require.ErrorIs(t, err, core.ErrMultipleMatches)

	all, err := core.NewQuery[Product](db).Sort("price", core.Asc).All(ctx)
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

	doc, inserted, err := core.NewQuery[Product](db, where.Field("name").Eq("Widget")).GetOrCreate(ctx, &Product{Name: "Widget", Price: 1.0})
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
	require.NoError(t, core.Save(ctx, db, existing))

	doc, inserted, err := core.NewQuery[Product](db, where.Field("name").Eq("Widget")).
		GetOrCreate(ctx, &Product{Name: "Widget", Price: 1.0}) // defaults — must NOT overwrite the 99.0
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

	doc, inserted, err := core.NewQuery[Product](db, where.Field("name").Eq("Widget")).UpsertOne(ctx, &Product{Name: "Widget", Price: 1.0}, core.SetFields{"price": 5.0})
	require.NoError(t, err)
	assert.True(t, inserted)
	assert.Equal(t, "Widget", doc.Name)
	assert.InDelta(t, 5.0, doc.Price, 0.001)
	assert.NotEmpty(t, doc.ID)

	found, err := core.FindByID[Product](ctx, db, doc.ID)
	require.NoError(t, err)
	assert.InDelta(t, 5.0, found.Price, 0.001)
}

func TestFindOneAndUpsert_Update(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	existing := &Product{Name: "Widget", Price: 1.0}
	require.NoError(t, core.Save(ctx, db, existing))

	doc, inserted, err := core.NewQuery[Product](db, where.Field("name").Eq("Widget")).
		UpsertOne(ctx,
			&Product{Name: "Widget", Price: 999.0}, // defaults must NOT apply on hit
			core.SetFields{"price": 5.0},
		)
	require.NoError(t, err)
	assert.False(t, inserted)
	assert.Equal(t, existing.ID, doc.ID)
	assert.InDelta(t, 5.0, doc.Price, 0.001)

	count, err := core.NewQuery[Product](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestFindOneAndUpsert_MultipleMatches(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	require.NoError(t, core.Save(ctx, db, &Product{Name: "Widget", Price: 1.0}))
	require.NoError(t, core.Save(ctx, db, &Product{Name: "Widget", Price: 2.0}))

	_, _, err := core.NewQuery[Product](db, where.Field("name").Eq("Widget")).UpsertOne(ctx, &Product{Name: "Widget"}, core.SetFields{"price": 99.0})
	require.ErrorIs(t, err, core.ErrMultipleMatches)
}

func TestFindOneAndUpsert_FieldNotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	_, _, err := core.NewQuery[Product](db, where.Field("name").Eq("Widget")).UpsertOne(ctx, &Product{Name: "Widget"}, core.SetFields{"nonexistent": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field")
}

func TestFindOneAndUpsert_SoftDeletedSkippedByDefault(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	original := &SoftProduct{Name: "Widget", Price: 1.0}
	require.NoError(t, core.Save(ctx, db, original))
	require.NoError(t, core.Delete(ctx, db, original))

	doc, inserted, err := core.NewQuery[SoftProduct](db, where.Field("name").Eq("Widget")).UpsertOne(ctx, &SoftProduct{Name: "Widget", Price: 10.0}, core.SetFields{"price": 20.0})
	require.NoError(t, err)
	assert.True(t, inserted, "soft-deleted match should not satisfy upsert")
	assert.NotEqual(t, original.ID, doc.ID)
	assert.InDelta(t, 20.0, doc.Price, 0.001)
}

func TestFindOneAndUpsert_IncludeDeletedUpdates(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	original := &SoftProduct{Name: "Widget", Price: 1.0}
	require.NoError(t, core.Save(ctx, db, original))
	require.NoError(t, core.Delete(ctx, db, original))

	doc, inserted, err := core.NewQuery[SoftProduct](db, where.Field("name").Eq("Widget")).
		IncludeDeleted().
		UpsertOne(ctx, &SoftProduct{Name: "Widget", Price: 10.0}, core.SetFields{"price": 20.0})
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

	_, inserted, err := core.NewQuery[orderingDoc](db, where.Field("name").Eq("Widget")).UpsertOne(ctx, &orderingDoc{Name: "Widget"}, core.SetFields{})
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

	require.NoError(t, core.Save(ctx, db, &orderingDoc{Name: "Widget"}))
	resetHookOrderCalls(t) // discard insert hooks from seed

	_, inserted, err := core.NewQuery[orderingDoc](db, where.Field("name").Eq("Widget")).UpsertOne(ctx, &orderingDoc{Name: "Widget"}, core.SetFields{})
	require.NoError(t, err)
	assert.False(t, inserted)
	assert.Equal(t,
		[]string{"BeforeUpdate", "BeforeSave", "Validate", "AfterUpdate", "AfterSave"},
		hookOrderCalls,
		"only Update hooks fire on hit path",
	)
}

func TestDelete_MissingIDWrapsErrValidation(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	err := core.Delete(ctx, db, &Product{Name: "NoID"})
	require.Error(t, err)
	require.ErrorIs(t, err, core.ErrValidation)
}

// --- den:"eager" honored across CRUD-style read APIs ---

// seedEagerHouse inserts one Door + one EagerHouse pointing at it,
// returning the persisted house. Used by the eager-CRUD tests below.
// EagerHouse / Door / EagerOwner are defined in link_test.go.
func seedEagerHouse(ctx context.Context, t *testing.T, db *core.DB, name string) *EagerHouse {
	t.Helper()
	door := &Door{Height: 200, Width: 80}
	require.NoError(t, core.Save(ctx, db, door))
	owner := &EagerOwner{Name: "Owner"}
	require.NoError(t, core.Save(ctx, db, owner))
	h := &EagerHouse{Name: name, Door: core.NewLink(door), Owner: core.NewLink(owner)}
	require.NoError(t, core.Save(ctx, db, h))
	return h
}

func TestFindByID_HonorsEagerTag(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	h := seedEagerHouse(ctx, t, db, "Cottage")

	got, err := core.FindByID[EagerHouse](ctx, db, h.ID)
	require.NoError(t, err)
	assert.True(t, got.Door.IsLoaded(), "FindByID must honor den:\"eager\" on Door")
	assert.False(t, got.Owner.IsLoaded(), "untagged Owner stays lazy")
}

func TestFindByID_WithoutFetchLinksSuppressesEager(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	h := seedEagerHouse(ctx, t, db, "Cottage")

	got, err := core.FindByID[EagerHouse](ctx, db, h.ID, core.WithoutFetchLinks())
	require.NoError(t, err)
	assert.False(t, got.Door.IsLoaded(), "WithoutFetchLinks must override eager tag")
	assert.NotEmpty(t, got.Door.ID, "ID still populated")
}

func TestFindByIDs_HonorsEagerTag(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	h1 := seedEagerHouse(ctx, t, db, "A")
	h2 := seedEagerHouse(ctx, t, db, "B")

	got, err := core.FindByIDs[EagerHouse](ctx, db, []string{h1.ID, h2.ID})
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

	require.NoError(t, core.Refresh(ctx, db, stale))
	assert.True(t, stale.Door.IsLoaded(), "Refresh must honor den:\"eager\"")
}

func TestFindOneAndUpdate_HonorsEagerTag(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	h := seedEagerHouse(ctx, t, db, "Cottage")

	got, err := core.NewQuery[EagerHouse](db, where.Field("_id").Eq(h.ID)).UpdateOne(ctx, core.SetFields{"name": "Renamed"})
	require.NoError(t, err)
	assert.Equal(t, "Renamed", got.Name)
	assert.True(t, got.Door.IsLoaded(), "FindOneAndUpdate must honor den:\"eager\"")
}

func TestFindOneAndUpsert_HonorsEagerTag_HitPath(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	h := seedEagerHouse(ctx, t, db, "Cottage")

	got, inserted, err := core.NewQuery[EagerHouse](db, where.Field("_id").Eq(h.ID)).UpsertOne(ctx, &EagerHouse{Name: "should-not-insert"}, core.SetFields{"name": "Updated"})
	require.NoError(t, err)
	assert.False(t, inserted)
	assert.True(t, got.Door.IsLoaded(), "Upsert hit path must honor den:\"eager\"")
}

func TestFindOneAndUpsert_HonorsEagerTag_MissPath(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	door := &Door{Height: 200, Width: 80}
	require.NoError(t, core.Save(ctx, db, door))

	defaults := &EagerHouse{Name: "Fresh", Door: core.NewLink(door)}
	got, inserted, err := core.NewQuery[EagerHouse](db, where.Field("name").Eq("Fresh")).UpsertOne(ctx, defaults, core.SetFields{})
	require.NoError(t, err)
	assert.True(t, inserted)
	assert.True(t, got.Door.IsLoaded(),
		"Upsert miss path must hydrate eager links on the freshly inserted doc")
}

func TestFindOrCreate_HonorsEagerTag(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &EagerOwner{}, &EagerHouse{})
	ctx := context.Background()

	h := seedEagerHouse(ctx, t, db, "Cottage")

	got, inserted, err := core.NewQuery[EagerHouse](db, where.Field("_id").Eq(h.ID)).GetOrCreate(ctx, &EagerHouse{Name: "should-not-insert"})
	require.NoError(t, err)
	assert.False(t, inserted)
	assert.True(t, got.Door.IsLoaded(),
		"FindOrCreate delegates to upsert; must honor den:\"eager\"")
}

// --- v0.12 Child 2: Save/SaveAll/DeleteAll/RefreshAll ---

func TestSaveAll_EmptyInput(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	require.NoError(t, core.SaveAll[Product](context.Background(), db, nil))
}

func TestSaveAll_MixedInsertAndUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	existing := &Product{Name: "Existing", Price: 1}
	require.NoError(t, core.Save(ctx, db, existing))

	// One new, one updating the existing.
	existing.Price = 99
	docs := []*Product{
		{Name: "Fresh", Price: 5}, // empty ID → Insert
		existing,                  // has ID → Update
	}
	require.NoError(t, core.SaveAll(ctx, db, docs))

	all, err := core.NewQuery[Product](db).Sort("name", core.Asc).All(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "Existing", all[0].Name)
	assert.InDelta(t, 99.0, all[0].Price, 0.001, "existing row's update must have taken effect")
	assert.Equal(t, "Fresh", all[1].Name)
}

type uniqueNameDoc struct {
	document.Base
	Name string `json:"name" den:"unique"`
}

func TestSaveAll_FailFastRollsBack(t *testing.T) {
	db := dentest.MustOpen(t, &uniqueNameDoc{})
	ctx := context.Background()

	// Pre-seed a doc that will collide with the second batch entry.
	require.NoError(t, core.Save(ctx, db, &uniqueNameDoc{Name: "Bad"}))

	docs := []*uniqueNameDoc{
		{Name: "Good"},
		{Name: "Bad"}, // duplicate of the pre-seeded row → ErrDuplicate on Insert
	}
	err := core.SaveAll(ctx, db, docs)
	require.Error(t, err, "duplicate name on the second doc must error")
	require.ErrorIs(t, err, core.ErrDuplicate)

	// Confirm the first doc was rolled back.
	count, err := core.NewQuery[uniqueNameDoc](db,
		where.Field("name").Eq("Good"),
	).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count, "fail-fast must roll back earlier inserts in the batch")
}

func TestDeleteAll_EmptyInput(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	require.NoError(t, core.DeleteAll[Product](context.Background(), db, nil))
}

func TestDeleteAll_BatchOfDocs(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	docs := []*Product{
		{Name: "A", Price: 1},
		{Name: "B", Price: 2},
		{Name: "C", Price: 3},
	}
	require.NoError(t, core.SaveAll(ctx, db, docs))

	require.NoError(t, core.DeleteAll(ctx, db, docs[:2])) // delete A and B

	remaining, err := core.NewQuery[Product](db).All(ctx)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, "C", remaining[0].Name)
}

func TestRefreshAll_EmptyInput(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	require.NoError(t, core.RefreshAll[Product](context.Background(), db, nil))
}

func TestRefreshAll_PicksUpExternalChanges(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	docs := []*Product{
		{Name: "A", Price: 1},
		{Name: "B", Price: 2},
	}
	require.NoError(t, core.SaveAll(ctx, db, docs))

	// External update via QuerySet.Update bumps both prices.
	_, err := core.NewQuery[Product](db).Update(ctx, core.SetFields{"price": 99.0})
	require.NoError(t, err)

	// Local docs still show the old prices.
	assert.InDelta(t, 1.0, docs[0].Price, 0.001)
	assert.InDelta(t, 2.0, docs[1].Price, 0.001)

	require.NoError(t, core.RefreshAll(ctx, db, docs))

	assert.InDelta(t, 99.0, docs[0].Price, 0.001)
	assert.InDelta(t, 99.0, docs[1].Price, 0.001)
}
