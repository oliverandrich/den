package migrate

import (
	"context"
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
