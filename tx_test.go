package den_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/where"
)

func TestRunInTransaction_Commit(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		found, err := den.FindByID[Product](ctx, tx, p.ID)
		if err != nil {
			return err
		}
		found.Price = 99.0
		return den.Update(ctx, tx, found)
	})
	require.NoError(t, err)

	found, err := den.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.InDelta(t, 99.0, found.Price, 0.001)
}

func TestRunInTransaction_Rollback(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		found, err := den.FindByID[Product](ctx, tx, p.ID)
		if err != nil {
			return err
		}
		found.Price = 99.0
		if err := den.Update(ctx, tx, found); err != nil {
			return err
		}
		return errors.New("abort")
	})
	require.Error(t, err)

	// Price should be unchanged
	found, err := den.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.InDelta(t, 10.0, found.Price, 0.001)
}

func TestTxInsert(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	var insertedID string
	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		p := &Product{Name: "InTx", Price: 42.0}
		if err := den.Insert(ctx, tx, p); err != nil {
			return err
		}
		insertedID = p.ID
		return nil
	})
	require.NoError(t, err)

	found, err := den.FindByID[Product](ctx, db, insertedID)
	require.NoError(t, err)
	assert.Equal(t, "InTx", found.Name)
}

func TestTxDelete(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "ToDelete", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		return den.Delete(ctx, tx, p)
	})
	require.NoError(t, err)

	_, err = den.FindByID[Product](ctx, db, p.ID)
	require.ErrorIs(t, err, den.ErrNotFound)
}

func TestTxDelete_SoftDelete(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	p := &SoftProduct{Name: "SoftInTx", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		return den.Delete(ctx, tx, p)
	})
	require.NoError(t, err)

	// Should be soft-deleted, not hard-deleted
	assert.True(t, p.IsDeleted())

	// FindByID still returns the document
	found, err := den.FindByID[SoftProduct](ctx, db, p.ID)
	require.NoError(t, err)
	assert.True(t, found.IsDeleted())

	// Should be hidden from normal queries
	results, err := den.NewQuery[SoftProduct](db).All(ctx)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestTxInsert_AfterHooks(t *testing.T) {
	db := dentest.MustOpen(t, &AfterSaveDoc{})
	ctx := context.Background()

	d := &AfterSaveDoc{Name: "InTx"}
	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		return den.Insert(ctx, tx, d)
	})
	require.NoError(t, err)
	assert.Equal(t, "called", d.SavedAt)
}

func TestTxUpdate_AfterHooks(t *testing.T) {
	db := dentest.MustOpen(t, &UpdateHookDoc{})
	ctx := context.Background()

	d := &UpdateHookDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d))

	d.Name = "Updated"
	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		return den.Update(ctx, tx, d)
	})
	require.NoError(t, err)
	assert.True(t, d.BeforeUpdated)
	assert.True(t, d.AfterUpdated)
}

func TestTxDelete_AfterHooks(t *testing.T) {
	db := dentest.MustOpen(t, &DeleteHookDoc{})
	ctx := context.Background()

	d := &DeleteHookDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d))

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		return den.Delete(ctx, tx, d)
	})
	require.NoError(t, err)
	assert.True(t, d.BeforeDeleted)
	assert.True(t, d.AfterDeleted)
}

func TestTxInsert_Revision(t *testing.T) {
	db := dentest.MustOpen(t, &RevProduct{})
	ctx := context.Background()

	p := &RevProduct{Name: "Widget", Price: 10.0}
	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		return den.Insert(ctx, tx, p)
	})
	require.NoError(t, err)
	assert.NotEmpty(t, p.Rev, "revision should be set on TxInsert")
}

func TestDeleteMany_SoftDelete(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	ctx := context.Background()

	products := []*SoftProduct{
		{Name: "Keep", Price: 5.0},
		{Name: "Delete1", Price: 15.0},
		{Name: "Delete2", Price: 25.0},
	}
	require.NoError(t, den.InsertMany(ctx, db, products))

	count, err := den.DeleteMany[SoftProduct](ctx, db, []where.Condition{where.Field("price").Gt(10.0)})
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	// Soft-deleted should be hidden from normal queries
	remaining, err := den.NewQuery[SoftProduct](db).All(ctx)
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, "Keep", remaining[0].Name)

	// But still accessible with IncludeDeleted
	all, err := den.NewQuery[SoftProduct](db).IncludeDeleted().All(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestRunInTransaction_PanicRecovery(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	// RunInTransaction catches panics, rolls back, and re-panics
	assert.Panics(t, func() {
		_ = den.RunInTransaction(ctx, db, func(_ *den.Tx) error {
			panic("unexpected panic")
		})
	})

	// Data should be unchanged after rollback
	found, err := den.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.InDelta(t, 10.0, found.Price, 0.001)
}

func TestTx_Transaction_RawRead(t *testing.T) {
	// Transaction() is the low-level escape hatch used by infrastructure
	// code like migrate. Verify a raw Get returns the stored JSON bytes.
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		data, err := tx.Transaction().Get(ctx, "product", p.ID)
		if err != nil {
			return err
		}
		assert.Contains(t, string(data), "Widget")
		return nil
	})
	require.NoError(t, err)
}

func TestTx_Transaction_RawWrite(t *testing.T) {
	// Writing raw bytes through tx.Transaction().Put bypasses encoding,
	// registry, hooks, and validation — same semantics the migration log
	// depends on.
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		return tx.Transaction().Put(ctx, "product", p.ID,
			[]byte(`{"_id":"`+p.ID+`","name":"Replaced","price":42}`))
	})
	require.NoError(t, err)

	found, err := den.FindByID[Product](ctx, db, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "Replaced", found.Name)
}

func TestTxFindByID_NotFound(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		_, err := den.FindByID[Product](ctx, tx, "nonexistent")
		return err
	})
	require.ErrorIs(t, err, den.ErrNotFound)
}

func TestInsertMany_Rollback(t *testing.T) {
	ctx := context.Background()

	// Use a hook that fails to trigger rollback.
	// FailBeforeDoc's BeforeInsert always returns error.
	db := dentest.MustOpen(t, &FailBeforeDoc{})
	products := []*FailBeforeDoc{
		{Name: "First"}, // BeforeInsert always fails
		{Name: "Second"},
	}
	err := den.InsertMany(ctx, db, products)
	require.Error(t, err)

	// No documents should persist after transaction rollback
	all, err := den.NewQuery[FailBeforeDoc](db).All(ctx)
	require.NoError(t, err)
	assert.Empty(t, all, "no documents should persist after transaction rollback")
}

func TestTxLockByID(t *testing.T) {
	dbs := map[string]*den.DB{
		"sqlite":   dentest.MustOpen(t, &Product{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Product{}),
	}
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := &Product{Name: "Widget", Price: 10.0}
			require.NoError(t, den.Insert(ctx, db, p))

			err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
				locked, err := den.LockByID[Product](ctx, tx, p.ID)
				if err != nil {
					return err
				}
				assert.Equal(t, p.ID, locked.ID)
				assert.Equal(t, "Widget", locked.Name)
				return nil
			})
			require.NoError(t, err)
		})
	}
}

func TestTxLockByID_NotFound(t *testing.T) {
	dbs := map[string]*den.DB{
		"sqlite":   dentest.MustOpen(t, &Product{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Product{}),
	}
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
				_, err := den.LockByID[Product](ctx, tx, "missing-id")
				return err
			})
			assert.ErrorIs(t, err, den.ErrNotFound)
		})
	}
}

func TestTxLockByID_SerializesConcurrentWriters(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Product{})
	ctx := context.Background()

	p := &Product{Name: "Contended", Price: 1.0}
	require.NoError(t, den.Insert(ctx, db, p))

	assertSerializesUnderContention(t, db, func(ctx context.Context, tx *den.Tx) error {
		_, err := den.LockByID[Product](ctx, tx, p.ID)
		return err
	})
}

// runContendedTx spawns a goroutine that holds a row lock until release is
// signaled. The returned release function must be called (deferred) to let the
// first transaction commit. locked is closed once tx1 has acquired the lock.
func runContendedTx(t *testing.T, db *den.DB, id string) (locked <-chan struct{}, release func()) {
	t.Helper()
	lockedCh := make(chan struct{})
	releaseCh := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		ctx := context.Background()
		err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
			if _, err := den.LockByID[Product](ctx, tx, id); err != nil {
				return err
			}
			close(lockedCh)
			<-releaseCh
			return nil
		})
		assert.NoError(t, err)
	}()

	t.Cleanup(func() {
		select {
		case <-releaseCh:
		default:
			close(releaseCh)
		}
		<-done
	})

	return lockedCh, func() { close(releaseCh) }
}

func TestTxLockByID_SkipLocked_ReturnsNotFoundOnContention(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Product{})
	ctx := context.Background()

	p := &Product{Name: "SkipLocked"}
	require.NoError(t, den.Insert(ctx, db, p))

	locked, release := runContendedTx(t, db, p.ID)
	<-locked

	start := time.Now()
	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		_, err := den.LockByID[Product](ctx, tx, p.ID, den.SkipLocked())
		return err
	})
	elapsed := time.Since(start)

	require.ErrorIs(t, err, den.ErrNotFound,
		"SkipLocked on a contended row should return ErrNotFound immediately")
	assert.Less(t, elapsed, 500*time.Millisecond,
		"SkipLocked must not block; returned after %v", elapsed)

	release()
}

func TestTxLockByID_NoWait_ReturnsErrLockedOnContention(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Product{})
	ctx := context.Background()

	p := &Product{Name: "NoWait"}
	require.NoError(t, den.Insert(ctx, db, p))

	locked, release := runContendedTx(t, db, p.ID)
	<-locked

	start := time.Now()
	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		_, err := den.LockByID[Product](ctx, tx, p.ID, den.NoWait())
		return err
	})
	elapsed := time.Since(start)

	require.ErrorIs(t, err, den.ErrLocked,
		"NoWait on a contended row should return ErrLocked immediately")
	assert.Less(t, elapsed, 500*time.Millisecond,
		"NoWait must not block; returned after %v", elapsed)

	release()
}

func TestTxLockByID_Options_SQLiteNoop(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Widget", Price: 10.0}
	require.NoError(t, den.Insert(ctx, db, p))

	for name, opt := range map[string]den.LockOption{
		"SkipLocked": den.SkipLocked(),
		"NoWait":     den.NoWait(),
	} {
		t.Run(name, func(t *testing.T) {
			err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
				locked, err := den.LockByID[Product](ctx, tx, p.ID, opt)
				if err != nil {
					return err
				}
				assert.Equal(t, "Widget", locked.Name)
				return nil
			})
			assert.NoError(t, err)
		})
	}
}

func TestTxLockByID_ConflictingOptions_Rejected(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	p := &Product{Name: "Conflict"}
	require.NoError(t, den.Insert(ctx, db, p))

	// SkipLocked and NoWait are mutually exclusive in PG; passing both used
	// to silently let the second win. Now it must return a clear error.
	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		_, err := den.LockByID[Product](ctx, tx, p.ID, den.SkipLocked(), den.NoWait())
		return err
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
	// Order of options must not matter.
	err = den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		_, err := den.LockByID[Product](ctx, tx, p.ID, den.NoWait(), den.SkipLocked())
		return err
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestLockByID_NGoroutines_ExactlyOneHolder(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Product{})
	ctx := context.Background()

	p := &Product{Name: "NWay"}
	require.NoError(t, den.Insert(ctx, db, p))

	const N = 5
	var currentHolders atomic.Int32

	var wg sync.WaitGroup
	for range N {
		wg.Go(func() {
			err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
				if _, err := den.LockByID[Product](ctx, tx, p.ID); err != nil {
					return err
				}
				if holders := currentHolders.Add(1); holders > 1 {
					t.Errorf("lock violation: %d concurrent holders", holders)
				}
				defer currentHolders.Add(-1)
				// Hold briefly so later goroutines queue on the lock.
				time.Sleep(20 * time.Millisecond)
				return nil
			})
			assert.NoError(t, err)
		})
	}
	wg.Wait()
}

// Test-only advisory-lock keys. Plain distinct values — no hidden encoding.
const (
	advisoryKeySerialize int64 = 101
	advisoryKeyParallelA int64 = 102
	advisoryKeyParallelB int64 = 103
	advisoryKeyRollback  int64 = 104
)

// assertSerializesUnderContention runs acquire in two concurrent transactions
// on db and verifies the second cannot proceed past acquire until the first
// commits. Both calls must target the same lockable object so they contend.
func assertSerializesUnderContention(t *testing.T, db *den.DB, acquire func(ctx context.Context, tx *den.Tx) error) {
	t.Helper()
	ctx := context.Background()

	var tx1Committed atomic.Bool
	var tx2AcquiredBeforeTx1Committed atomic.Bool
	tx1Released := make(chan struct{})

	var wg sync.WaitGroup
	wg.Go(func() {
		<-tx1Released
		err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
			if err := acquire(ctx, tx); err != nil {
				return err
			}
			if !tx1Committed.Load() {
				tx2AcquiredBeforeTx1Committed.Store(true)
			}
			return nil
		})
		assert.NoError(t, err)
	})

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		if err := acquire(ctx, tx); err != nil {
			return err
		}
		close(tx1Released)
		time.Sleep(150 * time.Millisecond)
		tx1Committed.Store(true)
		return nil
	})
	require.NoError(t, err)

	wg.Wait()
	assert.False(t, tx2AcquiredBeforeTx1Committed.Load(),
		"tx2 must block until tx1 commits")
}

func TestAdvisoryLock_SameKeySerializes(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Product{})
	assertSerializesUnderContention(t, db, func(ctx context.Context, tx *den.Tx) error {
		return den.AdvisoryLock(ctx, tx, advisoryKeySerialize)
	})
}

func TestAdvisoryLock_DifferentKeysParallel(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Product{})
	ctx := context.Background()

	const hold = 200 * time.Millisecond

	run := func(wg *sync.WaitGroup, key int64) {
		wg.Go(func() {
			err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
				if err := den.AdvisoryLock(ctx, tx, key); err != nil {
					return err
				}
				time.Sleep(hold)
				return nil
			})
			assert.NoError(t, err)
		})
	}

	start := time.Now()
	var wg sync.WaitGroup
	run(&wg, advisoryKeyParallelA)
	run(&wg, advisoryKeyParallelB)
	wg.Wait()
	elapsed := time.Since(start)

	// Serialized: ≈2*hold. Parallel: ≈hold. Allow 100ms scheduler slack.
	assert.Less(t, elapsed, 2*hold-100*time.Millisecond,
		"different keys must not serialize; took %v for 2×%v holds", elapsed, hold)
}

func TestAdvisoryLock_ReleasedOnRollback(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &Product{})
	ctx := context.Background()

	sentinel := errors.New("force rollback")

	tx1Released := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		<-tx1Released
		start := time.Now()
		err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
			return den.AdvisoryLock(ctx, tx, advisoryKeyRollback)
		})
		elapsed := time.Since(start)
		// Guard the timing check so it only runs when the tx itself succeeded;
		// require.* would only fail the goroutine, not the test.
		if assert.NoError(t, err) {
			assert.Less(t, elapsed, 500*time.Millisecond,
				"tx2 must acquire the lock shortly after tx1's rollback, took %v", elapsed)
		}
	})

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		if err := den.AdvisoryLock(ctx, tx, advisoryKeyRollback); err != nil {
			return err
		}
		close(tx1Released)
		// Give tx2 time to block on the lock before rolling back.
		time.Sleep(50 * time.Millisecond)
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	wg.Wait()
}

func TestAdvisoryLock_SQLiteNoop(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		return den.AdvisoryLock(ctx, tx, 42)
	})
	require.NoError(t, err,
		"SQLite AdvisoryLock is a no-op — IMMEDIATE transactions already serialize writers")
}

// TestScope_CRUDWorksWithBothDBAndTx proves that every unified CRUD entry
// point behaves identically whether the scope is *DB or *Tx. The same
// assertions run twice — once against the database directly, once against
// a transaction — so any future divergence between the two Scope paths
// would surface here. If this test ever breaks after a refactor, the
// compile-time interface is not enough to enforce behavior parity.
func TestScope_CRUDWorksWithBothDBAndTx(t *testing.T) {
	run := func(t *testing.T, ctx context.Context, s den.Scope) {
		t.Helper()
		p := &Product{Name: "Scope", Price: 12.5}
		require.NoError(t, den.Insert(ctx, s, p))
		require.NotEmpty(t, p.ID)

		got, err := den.FindByID[Product](ctx, s, p.ID)
		require.NoError(t, err)
		assert.Equal(t, "Scope", got.Name)

		got.Price = 20
		require.NoError(t, den.Update(ctx, s, got))

		refreshed, err := den.FindByID[Product](ctx, s, p.ID)
		require.NoError(t, err)
		assert.InDelta(t, 20.0, refreshed.Price, 0.001)

		require.NoError(t, den.Delete(ctx, s, refreshed))
		_, err = den.FindByID[Product](ctx, s, p.ID)
		require.ErrorIs(t, err, den.ErrNotFound)
	}

	ctx := context.Background()

	t.Run("scope=*DB", func(t *testing.T) {
		db := dentest.MustOpen(t, &Product{})
		run(t, ctx, db)
	})

	t.Run("scope=*Tx", func(t *testing.T) {
		db := dentest.MustOpen(t, &Product{})
		err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
			run(t, ctx, tx)
			return nil
		})
		require.NoError(t, err)
	})
}
