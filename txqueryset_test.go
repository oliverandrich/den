package den_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/where"
)

func TestNewTxQuery_All(t *testing.T) {
	dbs := map[string]*den.DB{
		"sqlite":   dentest.MustOpen(t, &Product{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Product{}),
	}
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, den.Insert(ctx, db, &Product{Name: "A", Price: 1}))
			require.NoError(t, den.Insert(ctx, db, &Product{Name: "B", Price: 2}))
			require.NoError(t, den.Insert(ctx, db, &Product{Name: "C", Price: 3}))

			err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
				items, err := den.NewQuery[Product](tx).
					Where(where.Field("price").Gte(2.0)).
					Sort("price", den.Asc).
					All(ctx)
				if err != nil {
					return err
				}
				require.Len(t, items, 2)
				assert.Equal(t, "B", items[0].Name)
				assert.Equal(t, "C", items[1].Name)
				return nil
			})
			require.NoError(t, err)
		})
	}
}

func TestNewTxQuery_First(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()
	require.NoError(t, den.Insert(ctx, db, &Product{Name: "Only"}))

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		found, err := den.NewQuery[Product](tx).First(ctx)
		if err != nil {
			return err
		}
		assert.Equal(t, "Only", found.Name)
		return nil
	})
	require.NoError(t, err)
}

func TestNewTxQuery_First_NotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		_, err := den.NewQuery[Product](tx).First(ctx)
		return err
	})
	require.ErrorIs(t, err, den.ErrNotFound)
}

func TestNewTxQuery_ForUpdate_SkipLocked(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Product{})
	ctx := context.Background()

	p1 := &Product{Name: "Taken"}
	p2 := &Product{Name: "Free"}
	require.NoError(t, den.Insert(ctx, db, p1))
	require.NoError(t, den.Insert(ctx, db, p2))

	locked, release := runContendedTx(t, db, p1.ID)
	<-locked

	start := time.Now()
	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		items, err := den.NewQuery[Product](tx).
			Sort("name", den.Asc).
			ForUpdate(den.SkipLocked()).
			All(ctx)
		if err != nil {
			return err
		}
		// p1 is locked → skipped. Only p2 should come back.
		require.Len(t, items, 1)
		assert.Equal(t, "Free", items[0].Name)
		return nil
	})
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.Less(t, elapsed, 500*time.Millisecond,
		"SkipLocked must not block; returned after %v", elapsed)

	release()
}

func TestNewTxQuery_ForUpdate_NoWait(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Product{})
	ctx := context.Background()

	p := &Product{Name: "Contended"}
	require.NoError(t, den.Insert(ctx, db, p))

	locked, release := runContendedTx(t, db, p.ID)
	<-locked

	start := time.Now()
	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		_, err := den.NewQuery[Product](tx).
			ForUpdate(den.NoWait()).
			All(ctx)
		return err
	})
	elapsed := time.Since(start)
	require.ErrorIs(t, err, den.ErrLocked)
	assert.Less(t, elapsed, 500*time.Millisecond,
		"NoWait must not block; returned after %v", elapsed)

	release()
}

func TestNewTxQuery_ForUpdate_ConflictingOptions(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()
	require.NoError(t, den.Insert(ctx, db, &Product{Name: "A"}))

	// The error is captured on the query set in ForUpdate and surfaces on
	// the terminal method — verify both All() and First() report it.
	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		_, err := den.NewQuery[Product](tx).
			ForUpdate(den.SkipLocked(), den.NoWait()).
			All(ctx)
		return err
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")

	err = den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		_, err := den.NewQuery[Product](tx).
			ForUpdate(den.NoWait(), den.SkipLocked()).
			First(ctx)
		return err
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// TestNewTxQuery_ForUpdate_OverlappingRowsNoDeadlock verifies that two
// concurrent ForUpdate().All(ctx) callers locking overlapping result sets do
// NOT deadlock. Without the default ORDER BY id in buildSelectSQL, PG would
// return rows in heap order and the two callers could acquire locks in
// different orders → 40P01.
func TestNewTxQuery_ForUpdate_OverlappingRowsNoDeadlock(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Product{})
	ctx := context.Background()

	// 20 rows: goroutine A locks price<=15 (16 rows), B locks price>=5
	// (16 rows) — 11 rows in common. Without deterministic ordering the
	// heap-order of each side's SELECT would differ and lock acquisition
	// would cross → deadlock.
	const N = 20
	for i := range N {
		require.NoError(t, den.Insert(ctx, db, &Product{
			Name:  "row",
			Price: float64(i),
		}))
	}

	// Serialize the two transactions enough to guarantee overlap: both
	// BEGIN, both try to lock, the loser waits for the winner. With the
	// fix neither deadlocks; without it the runtime reliably reports
	// 40P01 and one goroutine errors out.
	startA := make(chan struct{})
	startB := make(chan struct{})
	var wg sync.WaitGroup
	var errA, errB error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-startA
		errA = den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
			_, err := den.NewQuery[Product](tx).
				Where(where.Field("price").Lte(15.0)).
				ForUpdate().
				All(ctx)
			// Small hold to guarantee both TXs overlap in the lock window.
			time.Sleep(100 * time.Millisecond)
			return err
		})
	}()
	go func() {
		defer wg.Done()
		<-startB
		errB = den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
			_, err := den.NewQuery[Product](tx).
				Where(where.Field("price").Gte(5.0)).
				ForUpdate().
				All(ctx)
			time.Sleep(100 * time.Millisecond)
			return err
		})
	}()

	close(startA)
	time.Sleep(20 * time.Millisecond) // let A acquire first lock before B starts
	close(startB)
	wg.Wait()

	// Both must succeed: the second caller blocks briefly, then proceeds.
	require.NoError(t, errA, "goroutine A")
	require.NoError(t, errB, "goroutine B")
	// And specifically neither should surface a deadlock.
	require.NotErrorIs(t, errA, den.ErrDeadlock)
	require.NotErrorIs(t, errB, den.ErrDeadlock)
}

func TestNewTxQuery_ForUpdate_SQLiteNoop(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()
	require.NoError(t, den.Insert(ctx, db, &Product{Name: "A"}))
	require.NoError(t, den.Insert(ctx, db, &Product{Name: "B"}))

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		items, err := den.NewQuery[Product](tx).
			ForUpdate(den.SkipLocked()).
			All(ctx)
		if err != nil {
			return err
		}
		assert.Len(t, items, 2, "SQLite ignores lock modifiers")
		return nil
	})
	require.NoError(t, err)
}

// TestForUpdate_RequiresTransaction verifies the compile-time-plus-runtime
// safeguard: ForUpdate is legal syntactically on any QuerySet scope, but the
// terminal method refuses to execute when the scope is a *DB because a lock
// outside a transaction releases immediately and would be meaningless. The
// previous API enforced this at the type level (separate TxQuerySet type);
// the unified API enforces it via ErrLockRequiresTransaction at terminal time.
func TestForUpdate_RequiresTransaction(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()
	require.NoError(t, den.Insert(ctx, db, &Product{Name: "A"}))

	_, err := den.NewQuery[Product](db).ForUpdate().All(ctx)
	require.ErrorIs(t, err, den.ErrLockRequiresTransaction)

	_, err = den.NewQuery[Product](db).ForUpdate(den.SkipLocked()).First(ctx)
	require.ErrorIs(t, err, den.ErrLockRequiresTransaction)

	// Count doesn't actually consult Lock at the SQL level, but the preflight
	// should still surface the mismatch for callers who typed .Count(ctx)
	// after .ForUpdate() by mistake.
	_, err = den.NewQuery[Product](db).ForUpdate().Count(ctx)
	require.ErrorIs(t, err, den.ErrLockRequiresTransaction)
}
