package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
)

// newMockBackend wires a pgxmock-backed pool into a *backend that
// mirrors what production code receives. ExpectationsWereMet is asserted
// on cleanup so any missing expectation fails the owning test.
func newMockBackend(t *testing.T) (*backend, pgxmock.PgxPoolIface) {
	t.Helper()
	mock, err := pgxmock.NewPool(pgxmock.QueryMatcherOption(pgxmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet pgxmock expectations: %v", err)
		}
		mock.Close()
	})
	return &backend{pool: mock}, mock
}

// --- 1. Advisory lock SQL emission ------------------------------------

func TestTransaction_AdvisoryLock_EmitsExpectedSQL(t *testing.T) {
	b, mock := newMockBackend(t)
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock($1)").
		WithArgs(int64(42)).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))

	ctx := context.Background()
	tx, err := b.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	require.NoError(t, tx.AdvisoryLock(ctx, 42))
}

// --- 2. GetForUpdate lock-mode SQL ------------------------------------

func TestTransaction_GetForUpdate_LockModeSQL(t *testing.T) {
	cases := []struct {
		name string
		mode den.LockMode
		sql  string
	}{
		{"default", den.LockDefault, `SELECT data::text FROM "test_items" WHERE id = $1 FOR UPDATE`},
		{"skip-locked", den.LockSkipLocked, `SELECT data::text FROM "test_items" WHERE id = $1 FOR UPDATE SKIP LOCKED`},
		{"no-wait", den.LockNoWait, `SELECT data::text FROM "test_items" WHERE id = $1 FOR UPDATE NOWAIT`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, mock := newMockBackend(t)
			mock.ExpectBegin()
			mock.ExpectQuery(c.sql).
				WithArgs("id-1").
				WillReturnRows(pgxmock.NewRows([]string{"data"}).AddRow([]byte(`{"_id":"id-1"}`)))

			ctx := context.Background()
			tx, err := b.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()

			data, err := tx.GetForUpdate(ctx, "test_items", "id-1", c.mode)
			require.NoError(t, err)
			assert.JSONEq(t, `{"_id":"id-1"}`, string(data))
		})
	}
}

func TestTransaction_GetForUpdate_NoWait_PropagatesLockNotAvailable(t *testing.T) {
	b, mock := newMockBackend(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT data::text FROM "test_items" WHERE id = $1 FOR UPDATE NOWAIT`).
		WithArgs("id-1").
		WillReturnError(&pgconn.PgError{Code: "55P03", Message: "lock_not_available"})

	ctx := context.Background()
	tx, err := b.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	_, err = tx.GetForUpdate(ctx, "test_items", "id-1", den.LockNoWait)
	require.Error(t, err)
	assert.ErrorIs(t, err, den.ErrLocked)
}

// --- 3. Iterator surfaces a mid-stream row error ----------------------

func TestBackend_Query_IteratorSurfacesMidStreamRowError(t *testing.T) {
	b, mock := newMockBackend(t)
	mock.ExpectQuery(`SELECT id, data::text FROM "test_items"`).
		WillReturnRows(
			pgxmock.NewRows([]string{"id", "data"}).
				AddRow("id-1", []byte(`{"_id":"id-1"}`)).
				AddRow("id-2", []byte(`{"_id":"id-2"}`)).
				RowError(1, errors.New("driver boom")),
		)

	ctx := context.Background()
	iter, err := b.Query(ctx, "test_items", &den.Query{})
	require.NoError(t, err)
	defer func() { _ = iter.Close() }()

	require.True(t, iter.Next(), "first row should iterate cleanly")
	assert.Equal(t, "id-1", iter.ID())

	assert.False(t, iter.Next(), "iteration must stop at the failing row")
	require.Error(t, iter.Err(), "Err() must surface the mid-stream driver error")
	assert.Contains(t, iter.Err().Error(), "driver boom")
}

// --- 4. Pool acquire error propagates without panic -------------------

func TestBackend_Query_AcquireErrorPropagates(t *testing.T) {
	b, mock := newMockBackend(t)
	mock.ExpectQuery(`SELECT id, data::text FROM "test_items"`).
		WillReturnError(errors.New("acquire: pool exhausted"))

	ctx := context.Background()
	iter, err := b.Query(ctx, "test_items", &den.Query{})
	require.Error(t, err)
	assert.Nil(t, iter)
	assert.Contains(t, err.Error(), "pool exhausted")
}
