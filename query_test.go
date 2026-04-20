package den_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/where"
)

type QueryProduct struct {
	document.Base
	Name     string  `json:"name" den:"index"`
	Price    float64 `json:"price" den:"index"`
	Category string  `json:"category"`
}

func seedQueryProducts(t *testing.T, db *den.DB) {
	t.Helper()
	ctx := context.Background()
	products := []QueryProduct{
		{Name: "Alpha", Price: 10.0, Category: "A"},
		{Name: "Beta", Price: 20.0, Category: "B"},
		{Name: "Gamma", Price: 30.0, Category: "A"},
		{Name: "Delta", Price: 15.0, Category: "B"},
		{Name: "Epsilon", Price: 25.0, Category: "A"},
	}
	for i := range products {
		require.NoError(t, den.Insert(ctx, db, &products[i]))
	}
}

func TestFind_All(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](db).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 5)
}

func TestFind_WithCondition(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](db, where.Field("category").Eq("A")).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 3)
	for _, r := range results {
		assert.Equal(t, "A", r.Category)
	}
}

func TestFind_SortAsc(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](db).Sort("price", den.Asc).All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 5)
	assert.InDelta(t, 10.0, results[0].Price, 0.001)
	assert.InDelta(t, 30.0, results[4].Price, 0.001)
}

func TestFind_SortDesc(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](db).Sort("price", den.Desc).All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 5)
	assert.InDelta(t, 30.0, results[0].Price, 0.001)
	assert.InDelta(t, 10.0, results[4].Price, 0.001)
}

func TestFind_Limit(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](db).Sort("price", den.Asc).Limit(3).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestFind_Skip(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](db).Sort("price", den.Asc).Skip(2).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 3)
	assert.InDelta(t, 20.0, results[0].Price, 0.001)
}

func TestFind_RangeCondition(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](db,
		where.Field("price").Gte(15.0),
		where.Field("price").Lte(25.0),
	).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 3) // 15, 20, 25
}

func TestFindOne(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	p, err := den.NewQuery[QueryProduct](db, where.Field("name").Eq("Beta")).First(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Beta", p.Name)
}

func TestFindOne_NotFound(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	_, err := den.NewQuery[QueryProduct](db, where.Field("name").Eq("Nonexistent")).First(ctx)
	require.ErrorIs(t, err, den.ErrNotFound)
}

func TestFindAll(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](db).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 5)
}

func TestCount(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	count, err := den.NewQuery[QueryProduct](db, where.Field("category").Eq("A")).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestCount_All(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	count, err := den.NewQuery[QueryProduct](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)
}

func TestExists(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	exists, err := den.NewQuery[QueryProduct](db, where.Field("name").Eq("Alpha")).Exists(ctx)
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = den.NewQuery[QueryProduct](db, where.Field("name").Eq("Nonexistent")).Exists(ctx)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestFind_CursorPagination(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	ctx := context.Background()

	p := &QueryProduct{Name: "Solo", Price: 1.0}
	require.NoError(t, den.Insert(ctx, db, p))

	// After a very high ID should return nothing
	results, err := den.NewQuery[QueryProduct](db).After("ZZZZZZZZZZZZZZZZZZZZZZZZZZ").All(ctx)
	require.NoError(t, err)
	assert.Empty(t, results)

	// Before a very low ID should return nothing
	results, err = den.NewQuery[QueryProduct](db).Before("00000000000000000000000000").All(ctx)
	require.NoError(t, err)
	assert.Empty(t, results)

	// After a very low ID should return all
	results, err = den.NewQuery[QueryProduct](db).After("00000000000000000000000000").All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestFind_CursorPagination_AfterAndBefore(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	// Get all products sorted by ID to have deterministic ordering
	all, err := den.NewQuery[QueryProduct](db).All(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(all), 3)

	// Sort by ID (ULIDs are lexicographically sortable)
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })

	// Both After and Before set — should exclude first and last
	results, err := den.NewQuery[QueryProduct](db).
		After(all[0].ID).
		Before(all[len(all)-1].ID).
		All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, len(all)-2)
}

func TestFindWithCount(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, total, err := den.NewQuery[QueryProduct](db, where.Field("category").Eq("A")).
		Sort("price", den.Asc).
		Limit(2).
		AllWithCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, results, 2)
}

// --- QuerySet chain API tests ---

func TestQuerySet_All(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](db).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 5)
}

func TestQuerySet_Where(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](db).
		Where(where.Field("category").Eq("A")).
		All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestQuerySet_SortAndLimit(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](db).
		Sort("price", den.Asc).
		Limit(2).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.InDelta(t, 10.0, results[0].Price, 0.001)
	assert.InDelta(t, 15.0, results[1].Price, 0.001)
}

func TestQuerySet_First(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	p, err := den.NewQuery[QueryProduct](db).
		Where(where.Field("name").Eq("Beta")).
		First(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Beta", p.Name)
}

func TestQuerySet_CountChain(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	count, err := den.NewQuery[QueryProduct](db).
		Where(where.Field("category").Eq("A")).
		Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestQuerySet_Exists(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	exists, err := den.NewQuery[QueryProduct](db).
		Where(where.Field("name").Eq("Alpha")).
		Exists(ctx)
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = den.NewQuery[QueryProduct](db).
		Where(where.Field("name").Eq("Nonexistent")).
		Exists(ctx)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestQuerySet_AllWithCount(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, total, err := den.NewQuery[QueryProduct](db).
		Where(where.Field("category").Eq("A")).
		Sort("price", den.Asc).
		Limit(2).
		AllWithCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, results, 2)
}

func TestQuerySet_Lazy(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	// Base query — no execution yet
	base := den.NewQuery[QueryProduct](db).
		Where(where.Field("category").Eq("A"))

	// Derive two different queries from the same base
	sorted, err := base.Sort("price", den.Asc).All(ctx)
	require.NoError(t, err)
	assert.InDelta(t, 10.0, sorted[0].Price, 0.001)

	count, err := base.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

// --- Update terminal method tests ---

func TestQuerySet_Update(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	count, err := den.NewQuery[QueryProduct](db, where.Field("category").Eq("A")).
		Update(ctx, den.SetFields{"category": "Z"})
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)

	// Verify the update
	results, err := den.NewQuery[QueryProduct](db, where.Field("category").Eq("Z")).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Original category B should be untouched
	results, err = den.NewQuery[QueryProduct](db, where.Field("category").Eq("B")).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestStringContains(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	// "Alpha" and "Gamma" both contain "a"
	results, err := den.NewQuery[QueryProduct](db, where.Field("name").StringContains("lpha")).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 1) // Alpha

	// "elt" matches Delta and Epsilon? No — only Delta
	results, err = den.NewQuery[QueryProduct](db, where.Field("name").StringContains("elt")).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 1) // Delta
}

func TestStartsWith(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](db, where.Field("name").StartsWith("Al")).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 1) // Alpha

	results, err = den.NewQuery[QueryProduct](db, where.Field("name").StartsWith("Ep")).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 1) // Epsilon
}

func TestEndsWith(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	results, err := den.NewQuery[QueryProduct](db, where.Field("name").EndsWith("ta")).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 2) // Beta, Delta
}

func TestQuerySet_Update_NoMatches(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	ctx := context.Background()

	count, err := den.NewQuery[QueryProduct](db, where.Field("name").Eq("Nonexistent")).
		Update(ctx, den.SetFields{"price": 99.0})
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestQuerySet_Update_InvalidField(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	_, err := den.NewQuery[QueryProduct](db).
		Update(ctx, den.SetFields{"nonexistent": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field")
}

// TestQuerySet_Update_InvalidField_NoMatches pins the documented contract that
// field-name validation runs before the write transaction opens — an invalid
// field must error even when the predicate matches zero rows.
func TestQuerySet_Update_InvalidField_NoMatches(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	ctx := context.Background()

	_, err := den.NewQuery[QueryProduct](db, where.Field("name").Eq("Nonexistent")).
		Update(ctx, den.SetFields{"nonexistent": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field")
}

func TestQuerySet_Update_TypeMismatch(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	_, err := den.NewQuery[QueryProduct](db).
		Update(ctx, den.SetFields{"price": "not-a-float"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot assign")
}

// TestQuerySet_Update_Postgres_NoDeadlock reproduces den-chpf: on PostgreSQL,
// pgx.Rows pins the transaction's connection until Close. The previous
// QuerySet.Update implementation called TxUpdate inside the iteration loop,
// on the same tx → the Exec waits for the connection → the cursor waits for
// Close. Result: deadlock. With a sufficiently large result set this manifests
// reliably. A short context deadline makes the failure mode visible.
func TestQuerySet_Update_Postgres_NoDeadlock(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &QueryProduct{})

	parent, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Seed a result set big enough that the iterator streams.
	for i := range 25 {
		p := &QueryProduct{Name: fmt.Sprintf("bulk-%02d", i), Price: float64(i), Category: "bulk"}
		require.NoError(t, den.Insert(parent, db, p))
	}

	count, err := den.NewQuery[QueryProduct](db, where.Field("category").Eq("bulk")).
		Update(parent, den.SetFields{"category": "done"})
	require.NoError(t, err)
	assert.Equal(t, int64(25), count)

	// Confirm the writes landed.
	remaining, err := den.NewQuery[QueryProduct](db, where.Field("category").Eq("bulk")).Count(parent)
	require.NoError(t, err)
	assert.Equal(t, int64(0), remaining)

	done, err := den.NewQuery[QueryProduct](db, where.Field("category").Eq("done")).Count(parent)
	require.NoError(t, err)
	assert.Equal(t, int64(25), done)
}

func TestQuerySet_Update_HookFailureRollsBack(t *testing.T) {
	db := dentest.MustOpen(t, &FailBeforeUpdateDoc{})
	ctx := context.Background()

	docs := []*FailBeforeUpdateDoc{
		{Name: "A"},
		{Name: "B"},
		{Name: "C"},
	}
	for _, d := range docs {
		require.NoError(t, den.Insert(ctx, db, d))
	}

	count, err := den.NewQuery[FailBeforeUpdateDoc](db).
		Update(ctx, den.SetFields{"name": "X"})
	require.Error(t, err)
	assert.Equal(t, int64(0), count, "fail-fast: tx rolled back, count is zero")

	all, err := den.NewQuery[FailBeforeUpdateDoc](db).Sort("name", den.Asc).All(ctx)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, []string{"A", "B", "C"}, []string{all[0].Name, all[1].Name, all[2].Name},
		"no document was modified")
}

func TestQuerySet_Update_OnTxScope_FailureRollsBackCallerTx(t *testing.T) {
	db := dentest.MustOpen(t, &FailBeforeUpdateDoc{})
	ctx := context.Background()

	require.NoError(t, den.Insert(ctx, db, &FailBeforeUpdateDoc{Name: "Seed"}))

	// A failing bulk Update inside an outer tx must roll back the whole tx,
	// including any prior writes done in that same tx.
	txErr := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		if err := den.Insert(ctx, tx, &FailBeforeUpdateDoc{Name: "TxOnly"}); err != nil {
			return err
		}
		_, err := den.NewQuery[FailBeforeUpdateDoc](tx).
			Update(ctx, den.SetFields{"name": "X"})
		return err
	})
	require.Error(t, txErr)

	count, err := den.NewQuery[FailBeforeUpdateDoc](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "only the pre-tx seed survives; TxOnly is rolled back")
}

// updateHookCounter records BeforeUpdate / AfterUpdate fires across the freshly
// decoded instances inside QuerySet.Update's loop. Tests touching it must NOT
// use t.Parallel — there is no synchronization.
var updateHookCounter struct {
	beforeUpdate int
	afterUpdate  int
}

type counterUpdateDoc struct {
	document.Base
	Name string `json:"name"`
}

func (d *counterUpdateDoc) BeforeUpdate(_ context.Context) error {
	updateHookCounter.beforeUpdate++
	return nil
}

func (d *counterUpdateDoc) AfterUpdate(_ context.Context) error {
	updateHookCounter.afterUpdate++
	return nil
}

func TestQuerySet_Update_HooksFirePerRow(t *testing.T) {
	db := dentest.MustOpen(t, &counterUpdateDoc{})
	ctx := context.Background()
	updateHookCounter = struct {
		beforeUpdate int
		afterUpdate  int
	}{}
	t.Cleanup(func() {
		updateHookCounter = struct {
			beforeUpdate int
			afterUpdate  int
		}{}
	})

	for _, name := range []string{"A", "B", "C"} {
		require.NoError(t, den.Insert(ctx, db, &counterUpdateDoc{Name: name}))
	}

	count, err := den.NewQuery[counterUpdateDoc](db).
		Update(ctx, den.SetFields{"name": "X"})
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
	assert.Equal(t, 3, updateHookCounter.beforeUpdate, "BeforeUpdate fires per row")
	assert.Equal(t, 3, updateHookCounter.afterUpdate, "AfterUpdate fires per row")
}

func TestQuerySet_Update_HonorsCtxCancellation(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	ctx := context.Background()

	// Seed enough rows that the cancel-before-start case is meaningful.
	for i := range 5 {
		require.NoError(t, den.Insert(ctx, db, &QueryProduct{
			Name: fmt.Sprintf("p%d", i), Price: float64(i), Category: "bulk",
		}))
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	count, err := den.NewQuery[QueryProduct](db, where.Field("category").Eq("bulk")).
		Update(cancelCtx, den.SetFields{"category": "updated"})
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int64(0), count, "no rows updated when ctx is cancelled up front")

	// The batch transaction must roll back — no row survives the attempt.
	remaining, err := den.NewQuery[QueryProduct](db, where.Field("category").Eq("updated")).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), remaining)
}

// midLoopCancelFn is invoked by cancelOnBeforeUpdate's BeforeUpdate hook on
// the first document it sees, so tests can force ctx cancellation mid-batch
// without juggling goroutines. Tests touching it must NOT use t.Parallel.
var midLoopCancelFn context.CancelFunc

type cancelOnBeforeUpdate struct {
	document.Base
	Name     string `json:"name"`
	Category string `json:"category"`
}

func (d *cancelOnBeforeUpdate) BeforeUpdate(_ context.Context) error {
	if midLoopCancelFn != nil {
		fire := midLoopCancelFn
		midLoopCancelFn = nil // fire only once, on the first doc
		fire()
	}
	return nil
}

func TestQuerySet_Update_HonorsCtxCancellation_MidLoop(t *testing.T) {
	db := dentest.MustOpen(t, &cancelOnBeforeUpdate{})
	ctx := context.Background()
	for i := range 5 {
		require.NoError(t, den.Insert(ctx, db, &cancelOnBeforeUpdate{
			Name: fmt.Sprintf("p%d", i), Category: "bulk",
		}))
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	midLoopCancelFn = cancel
	t.Cleanup(func() { midLoopCancelFn = nil })

	count, err := den.NewQuery[cancelOnBeforeUpdate](db, where.Field("category").Eq("bulk")).
		Update(cancelCtx, den.SetFields{"category": "updated"})
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int64(0), count, "outer return is (0, err) when body returns an error")

	updated, err := den.NewQuery[cancelOnBeforeUpdate](db, where.Field("category").Eq("updated")).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), updated,
		"mid-loop cancellation rolls the whole batch back — no row carries the new category")
}

func TestDeleteMany_HonorsCtxCancellation(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	ctx := context.Background()

	for i := range 5 {
		require.NoError(t, den.Insert(ctx, db, &QueryProduct{
			Name: fmt.Sprintf("p%d", i), Price: float64(i), Category: "bulk",
		}))
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	count, err := den.DeleteMany[QueryProduct](cancelCtx, db,
		[]where.Condition{where.Field("category").Eq("bulk")})
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int64(0), count, "no rows deleted when ctx is cancelled up front")

	remaining, err := den.NewQuery[QueryProduct](db).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), remaining, "batch tx rolled back — all rows survive")
}

func TestQuerySet_Update_NilValue(t *testing.T) {
	db := dentest.MustOpen(t, &QueryProduct{})
	seedQueryProducts(t, db)
	ctx := context.Background()

	// nil value should set the zero value, not panic
	_, err := den.NewQuery[QueryProduct](db, where.Field("name").Eq("Alpha")).
		Update(ctx, den.SetFields{"category": nil})
	require.NoError(t, err)

	results, err := den.NewQuery[QueryProduct](db, where.Field("name").Eq("Alpha")).All(ctx)
	require.NoError(t, err)
	for _, r := range results {
		assert.Empty(t, r.Category)
	}
}
