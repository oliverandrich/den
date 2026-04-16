package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/oliverandrich/den"
)

type transaction struct {
	tx     pgx.Tx
	ctx    context.Context
	parent *backend
}

func (t *transaction) Get(ctx context.Context, collection, id string) ([]byte, error) {
	var data []byte
	err := t.tx.QueryRow(ctx, t.parent.getSQLs(collection).get, id).Scan(&data)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, den.ErrNotFound
		}
		return nil, err
	}
	return data, nil
}

func (t *transaction) GetForUpdate(ctx context.Context, collection, id string, mode den.LockMode) ([]byte, error) {
	suffix := ""
	switch mode {
	case den.LockDefault:
		// no modifier
	case den.LockSkipLocked:
		suffix = " SKIP LOCKED"
	case den.LockNoWait:
		suffix = " NOWAIT"
	}
	query := fmt.Sprintf("SELECT data::text FROM %s WHERE id = $1 FOR UPDATE%s", quoteIdent(collection), suffix)

	var data []byte
	err := t.tx.QueryRow(ctx, query, id).Scan(&data)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, den.ErrNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
			return nil, den.ErrLocked
		}
		return nil, err
	}
	return data, nil
}

func (t *transaction) Put(ctx context.Context, collection, id string, data []byte) error {
	_, err := t.tx.Exec(ctx, t.parent.getSQLs(collection).put, id, data)
	return mapPGError(err)
}

func (t *transaction) Delete(ctx context.Context, collection, id string) error {
	_, err := t.tx.Exec(ctx, t.parent.getSQLs(collection).delete, id)
	return err
}

func (t *transaction) Query(ctx context.Context, collection string, q *den.Query) (den.Iterator, error) {
	sqlStr, args := buildSelectSQL(collection, q)
	rows, err := t.tx.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	return &rowsIterator{rows: rows}, nil
}

func (t *transaction) Count(ctx context.Context, collection string, q *den.Query) (int64, error) {
	sqlStr, args := buildCountSQL(collection, q)
	var count int64
	err := t.tx.QueryRow(ctx, sqlStr, args...).Scan(&count)
	return count, err
}

func (t *transaction) Exists(ctx context.Context, collection string, q *den.Query) (bool, error) {
	sqlStr, args := buildExistsSQL(collection, q)
	var exists bool
	err := t.tx.QueryRow(ctx, sqlStr, args...).Scan(&exists)
	return exists, err
}

func (t *transaction) Aggregate(ctx context.Context, collection string, op den.AggregateOp, field string, q *den.Query) (*float64, error) {
	sqlStr, args, err := buildAggregateSQL(collection, op, field, q)
	if err != nil {
		return nil, err
	}
	var result *float64
	err = t.tx.QueryRow(ctx, sqlStr, args...).Scan(&result)
	return result, err
}

func (t *transaction) GroupBy(ctx context.Context, collection string, groupField string, aggs []den.GroupByAgg, q *den.Query) ([]den.GroupByRow, error) {
	sqlStr, args, err := buildGroupBySQL(collection, groupField, aggs, q)
	if err != nil {
		return nil, err
	}
	return scanGroupByRowsPG(ctx, t.tx, sqlStr, args, len(aggs))
}

func (t *transaction) Commit() error {
	return t.tx.Commit(t.ctx)
}

func (t *transaction) Rollback() error {
	return t.tx.Rollback(t.ctx)
}
