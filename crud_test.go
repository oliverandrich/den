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
		where.Field("name").Eq("Widget"),
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
		where.Field("name").Eq("Nonexistent"),
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
		where.Field("name").Eq("Widget"),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field")
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
