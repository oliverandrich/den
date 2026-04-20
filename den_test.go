package den_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
)

func TestNewID_Length(t *testing.T) {
	id := den.NewID()
	assert.Len(t, id, 26, "ULID is a 26-char Crockford-base32 string")
}

func TestNewID_Unique(t *testing.T) {
	a := den.NewID()
	b := den.NewID()
	assert.NotEqual(t, a, b, "back-to-back NewID calls must not collide")
}

func TestNewID_BulkUnique(t *testing.T) {
	const N = 10000
	seen := make(map[string]struct{}, N)
	for range N {
		id := den.NewID()
		_, dup := seen[id]
		require.False(t, dup, "duplicate id %q at sample size %d", id, len(seen))
		seen[id] = struct{}{}
	}
	assert.Len(t, seen, N)
}

func TestNewID_TimeSortable(t *testing.T) {
	id1 := den.NewID()
	// ULIDs are only time-ordered at millisecond granularity — sleep past a
	// millisecond boundary so the second ID is guaranteed to have a later
	// timestamp prefix.
	time.Sleep(2 * time.Millisecond)
	id2 := den.NewID()

	assert.Less(t, id1, id2,
		"IDs from different milliseconds must be lexicographically ordered")
}

func TestWithTagValidator_InvokesOnInsert(t *testing.T) {
	var calls atomic.Int64
	validator := func(doc any) error {
		calls.Add(1)
		return nil
	}

	db := dentest.MustOpenWith(t,
		[]any{&Product{}},
		[]den.Option{den.WithTagValidator(validator)},
	)
	ctx := context.Background()

	require.NoError(t, den.Insert(ctx, db, &Product{Name: "A", Price: 1}))
	require.NoError(t, den.Insert(ctx, db, &Product{Name: "B", Price: 2}))

	assert.Equal(t, int64(2), calls.Load(),
		"validator must run once per Insert")
}

func TestWithTagValidator_WrapsErrorAsValidation(t *testing.T) {
	sentinel := errors.New("tag says no")
	validator := func(doc any) error { return sentinel }

	db := dentest.MustOpenWith(t,
		[]any{&Product{}},
		[]den.Option{den.WithTagValidator(validator)},
	)
	ctx := context.Background()

	err := den.Insert(ctx, db, &Product{Name: "X", Price: 1})
	require.Error(t, err)
	require.ErrorIs(t, err, den.ErrValidation,
		"validator errors must wrap ErrValidation so callers can switch on it")
	require.ErrorIs(t, err, sentinel,
		"original validator error must remain reachable via errors.Is")
}

func TestWithoutTagValidator_Inserts(t *testing.T) {
	// Default Open (no WithTagValidator) must not panic and must allow Inserts
	// through without calling any tag validator.
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()

	require.NoError(t, den.Insert(ctx, db, &Product{Name: "A", Price: 1}))
}
