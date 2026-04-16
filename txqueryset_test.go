package den_test

import (
	"context"
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
				items, err := den.NewTxQuery[Product](tx).
					Where(where.Field("price").Gte(2.0)).
					Sort("price", den.Asc).
					All()
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
		found, err := den.NewTxQuery[Product](tx).First()
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
		_, err := den.NewTxQuery[Product](tx).First()
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
		items, err := den.NewTxQuery[Product](tx).
			Sort("name", den.Asc).
			ForUpdate(den.SkipLocked()).
			All()
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
		_, err := den.NewTxQuery[Product](tx).
			ForUpdate(den.NoWait()).
			All()
		return err
	})
	elapsed := time.Since(start)
	require.ErrorIs(t, err, den.ErrLocked)
	assert.Less(t, elapsed, 500*time.Millisecond,
		"NoWait must not block; returned after %v", elapsed)

	release()
}

func TestNewTxQuery_ForUpdate_SQLiteNoop(t *testing.T) {
	db := dentest.MustOpen(t, &Product{})
	ctx := context.Background()
	require.NoError(t, den.Insert(ctx, db, &Product{Name: "A"}))
	require.NoError(t, den.Insert(ctx, db, &Product{Name: "B"}))

	err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		items, err := den.NewTxQuery[Product](tx).
			ForUpdate(den.SkipLocked()).
			All()
		if err != nil {
			return err
		}
		assert.Len(t, items, 2, "SQLite ignores lock modifiers")
		return nil
	})
	require.NoError(t, err)
}
