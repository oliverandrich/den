// SPDX-License-Identifier: MIT

package sqlite

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
)

// newMockBackend wires a go-sqlmock-backed *sql.DB into a *backend.
// ExpectationsWereMet is asserted on cleanup so any missing expectation
// fails the owning test.
func newMockBackend(t *testing.T) (*backend, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet sqlmock expectations: %v", err)
		}
		_ = db.Close()
	})
	return &backend{db: db}, mock
}

// --- prepare-error paths in getStmts ----------------------------------

func TestGetStmts_FirstPrepareFails(t *testing.T) {
	b, mock := newMockBackend(t)
	mock.ExpectPrepare(`SELECT json(data) FROM "items" WHERE id = ?`).
		WillReturnError(errors.New("prepare get boom"))

	_, err := b.Get(t.Context(), "items", "id-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prepare get boom")
}

func TestGetStmts_SecondPrepareFails(t *testing.T) {
	// The second Prepare ("put") fails after the first ("get") succeeds — the
	// helper must close the already-prepared get statement before returning.
	b, mock := newMockBackend(t)
	mock.ExpectPrepare(`SELECT json(data) FROM "items" WHERE id = ?`)
	mock.ExpectPrepare(`INSERT INTO "items" (id, data) VALUES (?, jsonb(?)) ON CONFLICT(id) DO UPDATE SET data = jsonb(?)`).
		WillReturnError(errors.New("prepare put boom"))

	err := b.Put(t.Context(), "items", "id-1", []byte(`{"x":1}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prepare put boom")
}

func TestGetStmts_ThirdPrepareFails(t *testing.T) {
	b, mock := newMockBackend(t)
	mock.ExpectPrepare(`SELECT json(data) FROM "items" WHERE id = ?`)
	mock.ExpectPrepare(`INSERT INTO "items" (id, data) VALUES (?, jsonb(?)) ON CONFLICT(id) DO UPDATE SET data = jsonb(?)`)
	mock.ExpectPrepare(`DELETE FROM "items" WHERE id = ?`).
		WillReturnError(errors.New("prepare delete boom"))

	err := b.Delete(t.Context(), "items", "id-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prepare delete boom")
}

// --- exec-error paths through prepared statements ---------------------

// expectAllPrepares registers the three Prepare expectations that getStmts
// emits on first use of the "items" collection (get, put, delete in that
// order). Tests can layer query/exec expectations on top.
func expectAllPrepares(mock sqlmock.Sqlmock) {
	mock.ExpectPrepare(`SELECT json(data) FROM "items" WHERE id = ?`)
	mock.ExpectPrepare(`INSERT INTO "items" (id, data) VALUES (?, jsonb(?)) ON CONFLICT(id) DO UPDATE SET data = jsonb(?)`)
	mock.ExpectPrepare(`DELETE FROM "items" WHERE id = ?`)
}

func TestPut_ExecError(t *testing.T) {
	b, mock := newMockBackend(t)
	expectAllPrepares(mock)
	mock.ExpectExec(`INSERT INTO "items" (id, data) VALUES (?, jsonb(?)) ON CONFLICT(id) DO UPDATE SET data = jsonb(?)`).
		WithArgs("id-1", []byte(`{"x":1}`), []byte(`{"x":1}`)).
		WillReturnError(errors.New("disk full"))

	err := b.Put(t.Context(), "items", "id-1", []byte(`{"x":1}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}

func TestDelete_ExecError(t *testing.T) {
	b, mock := newMockBackend(t)
	expectAllPrepares(mock)
	mock.ExpectExec(`DELETE FROM "items" WHERE id = ?`).
		WithArgs("id-1").
		WillReturnError(errors.New("delete boom"))

	err := b.Delete(t.Context(), "items", "id-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete boom")
}

// --- Get error mapping ------------------------------------------------

func TestGet_NotFoundMaps(t *testing.T) {
	b, mock := newMockBackend(t)
	expectAllPrepares(mock)
	mock.ExpectQuery(`SELECT json(data) FROM "items" WHERE id = ?`).
		WithArgs("id-1").
		WillReturnError(sql.ErrNoRows)

	_, err := b.Get(t.Context(), "items", "id-1")
	require.Error(t, err)
	assert.ErrorIs(t, err, den.ErrNotFound)
}

func TestGet_NonNotFoundErrorPassesThrough(t *testing.T) {
	b, mock := newMockBackend(t)
	expectAllPrepares(mock)
	mock.ExpectQuery(`SELECT json(data) FROM "items" WHERE id = ?`).
		WithArgs("id-1").
		WillReturnError(errors.New("io error"))

	_, err := b.Get(t.Context(), "items", "id-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "io error")
	assert.NotErrorIs(t, err, den.ErrNotFound)
}

// --- Query / Count / Exists / DropIndex error paths -------------------

func TestQuery_QueryError(t *testing.T) {
	b, mock := newMockBackend(t)
	mock.ExpectQuery(`SELECT id, json(data) FROM "items"`).
		WillReturnError(errors.New("query boom"))

	iter, err := b.Query(t.Context(), "items", &den.Query{})
	require.Error(t, err)
	assert.Nil(t, iter)
	assert.Contains(t, err.Error(), "query boom")
}

func TestQuery_IteratorSurfacesMidStreamRowError(t *testing.T) {
	b, mock := newMockBackend(t)
	mock.ExpectQuery(`SELECT id, json(data) FROM "items"`).
		WillReturnRows(
			sqlmock.NewRows([]string{"id", "data"}).
				AddRow("id-1", []byte(`{"_id":"id-1"}`)).
				AddRow("id-2", []byte(`{"_id":"id-2"}`)).
				RowError(1, errors.New("driver boom")),
		)

	iter, err := b.Query(t.Context(), "items", &den.Query{})
	require.NoError(t, err)
	defer func() { _ = iter.Close() }()

	require.True(t, iter.Next(), "first row should iterate cleanly")
	assert.Equal(t, "id-1", iter.ID())

	assert.False(t, iter.Next(), "iteration must stop at the failing row")
	require.Error(t, iter.Err())
	assert.Contains(t, iter.Err().Error(), "driver boom")
}

func TestCount_ScanError(t *testing.T) {
	b, mock := newMockBackend(t)
	mock.ExpectQuery(`SELECT COUNT(*) FROM "items"`).
		WillReturnError(errors.New("count boom"))

	_, err := b.Count(t.Context(), "items", &den.Query{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "count boom")
}

func TestExists_ScanError(t *testing.T) {
	b, mock := newMockBackend(t)
	mock.ExpectQuery(`SELECT EXISTS(SELECT 1 FROM "items" LIMIT 1)`).
		WillReturnError(errors.New("exists boom"))

	_, err := b.Exists(t.Context(), "items", &den.Query{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exists boom")
}

func TestDropIndex_ExecError(t *testing.T) {
	b, mock := newMockBackend(t)
	mock.ExpectExec(`DROP INDEX IF EXISTS "idx_foo"`).
		WillReturnError(errors.New("drop boom"))

	err := b.DropIndex(t.Context(), "items", "idx_foo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "drop boom")
}

func TestAggregate_ScanError(t *testing.T) {
	b, mock := newMockBackend(t)
	mock.ExpectQuery(`SELECT SUM(CAST(json_extract(data, '$.price') AS REAL)) FROM "items"`).
		WillReturnError(errors.New("agg boom"))

	_, err := b.Aggregate(t.Context(), "items", den.OpSum, "price", &den.Query{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agg boom")
}

func TestGroupBy_QueryError(t *testing.T) {
	b, mock := newMockBackend(t)
	mock.ExpectQuery(`SELECT json_extract(data, '$.category'), COUNT(*) FROM "items" GROUP BY json_extract(data, '$.category')`).
		WillReturnError(errors.New("groupby boom"))

	_, err := b.GroupBy(t.Context(), "items",
		[]string{"category"}, []den.GroupByAgg{{Op: den.OpCount}}, &den.Query{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "groupby boom")
}
