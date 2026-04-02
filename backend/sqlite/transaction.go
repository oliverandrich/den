package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oliverandrich/den"
)

type transaction struct {
	tx     *sql.Tx
	parent *backend
}

func (t *transaction) Get(ctx context.Context, collection, id string) ([]byte, error) {
	var data []byte
	err := t.tx.QueryRowContext(ctx,
		fmt.Sprintf("SELECT json(data) FROM %q WHERE id = ?", collection), id,
	).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, den.ErrNotFound
		}
		return nil, err
	}
	return data, nil
}

func (t *transaction) Put(ctx context.Context, collection, id string, data []byte) error {
	_, err := t.tx.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %q (id, data) VALUES (?, jsonb(?)) ON CONFLICT(id) DO UPDATE SET data = jsonb(?)", collection),
		id, data, data,
	)
	return mapSQLiteError(err)
}

func (t *transaction) Delete(ctx context.Context, collection, id string) error {
	_, err := t.tx.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM %q WHERE id = ?", collection), id,
	)
	return err
}

func (t *transaction) Query(ctx context.Context, collection string, q *den.Query) (den.Iterator, error) {
	sqlStr, args := buildSelectSQL(collection, q)
	rows, err := t.tx.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	return &rowsIterator{rows: rows}, nil
}

func (t *transaction) Count(ctx context.Context, collection string, q *den.Query) (int64, error) {
	sqlStr, args := buildCountSQL(collection, q)
	var count int64
	err := t.tx.QueryRowContext(ctx, sqlStr, args...).Scan(&count)
	return count, err
}

func (t *transaction) Exists(ctx context.Context, collection string, q *den.Query) (bool, error) {
	sqlStr, args := buildExistsSQL(collection, q)
	var exists bool
	err := t.tx.QueryRowContext(ctx, sqlStr, args...).Scan(&exists)
	return exists, err
}

func (t *transaction) Aggregate(ctx context.Context, collection string, op den.AggregateOp, field string, q *den.Query) (*float64, error) {
	sqlStr, args := buildAggregateSQL(collection, op, field, q)
	var result *float64
	err := t.tx.QueryRowContext(ctx, sqlStr, args...).Scan(&result)
	return result, err
}

func (t *transaction) Commit() error {
	return t.tx.Commit()
}

func (t *transaction) Rollback() error {
	return t.tx.Rollback()
}
