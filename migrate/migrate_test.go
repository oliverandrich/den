package migrate

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
)

func TestRegisterAndUp(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	var called []string

	r.Register("001_first", Migration{
		Forward: func(_ context.Context, _ *den.Tx) error {
			called = append(called, "001")
			return nil
		},
	})
	r.Register("002_second", Migration{
		Forward: func(_ context.Context, _ *den.Tx) error {
			called = append(called, "002")
			return nil
		},
	})

	db := dentest.MustOpen(t)
	require.NoError(t, r.Up(ctx, db))

	assert.Equal(t, []string{"001", "002"}, called)
}

func TestUp_SkipsApplied(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	count := 0

	r.Register("001_first", Migration{
		Forward: func(_ context.Context, _ *den.Tx) error {
			count++
			return nil
		},
	})

	db := dentest.MustOpen(t)
	require.NoError(t, r.Up(ctx, db))
	require.NoError(t, r.Up(ctx, db)) // second run should skip

	assert.Equal(t, 1, count)
}

func TestUpOne(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	var called []string

	r.Register("001_first", Migration{
		Forward: func(_ context.Context, _ *den.Tx) error {
			called = append(called, "001")
			return nil
		},
	})
	r.Register("002_second", Migration{
		Forward: func(_ context.Context, _ *den.Tx) error {
			called = append(called, "002")
			return nil
		},
	})

	db := dentest.MustOpen(t)
	require.NoError(t, r.UpOne(ctx, db))
	assert.Equal(t, []string{"001"}, called)

	require.NoError(t, r.UpOne(ctx, db))
	assert.Equal(t, []string{"001", "002"}, called)
}

func TestDownOne(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	var calls []string

	r.Register("001_first", Migration{
		Forward: func(_ context.Context, _ *den.Tx) error {
			calls = append(calls, "up-001")
			return nil
		},
		Backward: func(_ context.Context, _ *den.Tx) error {
			calls = append(calls, "down-001")
			return nil
		},
	})

	db := dentest.MustOpen(t)
	require.NoError(t, r.Up(ctx, db))
	require.NoError(t, r.DownOne(ctx, db))

	assert.Contains(t, calls, "down-001")
}

func TestDown(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	var calls []string

	r.Register("001_first", Migration{
		Forward: func(_ context.Context, _ *den.Tx) error {
			calls = append(calls, "up-001")
			return nil
		},
		Backward: func(_ context.Context, _ *den.Tx) error {
			calls = append(calls, "down-001")
			return nil
		},
	})
	r.Register("002_second", Migration{
		Forward: func(_ context.Context, _ *den.Tx) error {
			calls = append(calls, "up-002")
			return nil
		},
		Backward: func(_ context.Context, _ *den.Tx) error {
			calls = append(calls, "down-002")
			return nil
		},
	})

	db := dentest.MustOpen(t)
	require.NoError(t, r.Up(ctx, db))
	require.NoError(t, r.Down(ctx, db))

	// Down should roll back in reverse order
	assert.Equal(t, "down-002", calls[2])
	assert.Equal(t, "down-001", calls[3])
}

func TestMigration_ForwardError(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()

	r.Register("001_fail", Migration{
		Forward: func(_ context.Context, _ *den.Tx) error {
			return assert.AnError
		},
	})

	db := dentest.MustOpen(t)
	err := r.Up(ctx, db)
	require.Error(t, err)
}

func TestUpOne_NoPending(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	db := dentest.MustOpen(t)

	err := r.UpOne(ctx, db)
	assert.NoError(t, err) // no-op when nothing to apply
}

func TestDownOne_NoneApplied(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	db := dentest.MustOpen(t)

	err := r.DownOne(ctx, db)
	assert.NoError(t, err) // no-op when nothing to rollback
}

func TestDownOne_NoBackwardFunction(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()

	r.Register("001_first", Migration{
		Forward: func(_ context.Context, _ *den.Tx) error { return nil },
		// No Backward
	})

	db := dentest.MustOpen(t)
	require.NoError(t, r.Up(ctx, db))

	err := r.DownOne(ctx, db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no backward function")
}

func TestDownOne_BackwardError(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()

	r.Register("001_first", Migration{
		Forward: func(_ context.Context, _ *den.Tx) error { return nil },
		Backward: func(_ context.Context, _ *den.Tx) error {
			return assert.AnError
		},
	})

	db := dentest.MustOpen(t)
	require.NoError(t, r.Up(ctx, db))

	err := r.DownOne(ctx, db)
	require.Error(t, err)
	require.ErrorIs(t, err, den.ErrMigrationFailed)
}

func TestDown_NoBackwardFunction(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()

	r.Register("001_first", Migration{
		Forward: func(_ context.Context, _ *den.Tx) error { return nil },
		// No Backward
	})

	db := dentest.MustOpen(t)
	require.NoError(t, r.Up(ctx, db))

	err := r.Down(ctx, db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no backward function")
}

// concurrentUpTest runs N goroutines calling Up concurrently against the
// same DB. Each migration's Forward must be called exactly once even under
// contention — two concurrent starters must not both run the same version.
func concurrentUpTest(t *testing.T, db *den.DB) {
	t.Helper()

	const goroutines = 8
	const migrations = 3

	// counters[i] is bumped inside migration i's Forward; after the test
	// each must be exactly 1.
	var counters [migrations]atomic.Int32

	r := NewRegistry()
	for i := range migrations {
		idx := i
		r.Register(versionName(idx), Migration{
			Forward: func(_ context.Context, _ *den.Tx) error {
				counters[idx].Add(1)
				return nil
			},
		})
	}

	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for g := range goroutines {
		wg.Add(1)
		go func(gi int) {
			defer wg.Done()
			errs[gi] = r.Up(context.Background(), db)
		}(g)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoErrorf(t, err, "goroutine %d", i)
	}
	for i := range counters {
		assert.Equalf(t, int32(1), counters[i].Load(),
			"migration %d must run exactly once across %d concurrent starters", i, goroutines)
	}
}

func versionName(i int) string {
	return []string{"001_a", "002_b", "003_c"}[i]
}

func TestUp_ConcurrentStarters_SQLite(t *testing.T) {
	db := dentest.MustOpen(t)
	concurrentUpTest(t, db)
}

func TestUp_ConcurrentStarters_Postgres(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL())
	// dentest's PG cleanup only drops registered collections; _den_migrations
	// is unregistered, so entries from prior test runs stick. Reset here so
	// each run starts from an empty applied set.
	require.NoError(t, db.Backend().DropCollection(context.Background(), "_den_migrations"))
	concurrentUpTest(t, db)
}

func TestDown_BackwardError(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()

	r.Register("001_first", Migration{
		Forward: func(_ context.Context, _ *den.Tx) error { return nil },
		Backward: func(_ context.Context, _ *den.Tx) error {
			return assert.AnError
		},
	})

	db := dentest.MustOpen(t)
	require.NoError(t, r.Up(ctx, db))

	err := r.Down(ctx, db)
	require.Error(t, err)
	require.ErrorIs(t, err, den.ErrMigrationFailed)
}
