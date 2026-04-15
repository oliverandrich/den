package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oliverandrich/den"
)

func init() {
	den.RegisterBackend("postgres", func(dsn string) (den.Backend, error) { return Open(dsn) })
	den.RegisterBackend("postgresql", func(dsn string) (den.Backend, error) { return Open(dsn) })
}

type sqlSet struct {
	get    string
	put    string
	delete string
}

type backend struct {
	pool *pgxpool.Pool
	sqls sync.Map // collection name → *sqlSet
}

// Open connects to a PostgreSQL database using the given connection string.
// It verifies that the server version meets the minimum requirement.
func Open(connString string) (den.Backend, error) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("postgres open: %w", err)
	}

	versionNum, err := serverVersion(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres open: %w", err)
	}
	if err := checkMinVersion(versionNum); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres open: %w", err)
	}

	return &backend{pool: pool}, nil
}

func (b *backend) Encoder() den.Encoder {
	return newJSONEncoder()
}

func (b *backend) getSQLs(collection string) *sqlSet {
	if v, ok := b.sqls.Load(collection); ok {
		s, _ := v.(*sqlSet)
		return s
	}
	q := quoteIdent(collection)
	set := &sqlSet{
		get:    fmt.Sprintf("SELECT data::text FROM %s WHERE id = $1", q),
		put:    fmt.Sprintf("INSERT INTO %s (id, data) VALUES ($1, $2::jsonb) ON CONFLICT (id) DO UPDATE SET data = $2::jsonb", q),
		delete: fmt.Sprintf("DELETE FROM %s WHERE id = $1", q),
	}
	actual, _ := b.sqls.LoadOrStore(collection, set)
	s, _ := actual.(*sqlSet)
	return s
}

func (b *backend) Get(ctx context.Context, collection, id string) ([]byte, error) {
	var data []byte
	err := b.pool.QueryRow(ctx, b.getSQLs(collection).get, id).Scan(&data)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, den.ErrNotFound
		}
		return nil, err
	}
	return data, nil
}

func (b *backend) Put(ctx context.Context, collection, id string, data []byte) error {
	_, err := b.pool.Exec(ctx, b.getSQLs(collection).put, id, data)
	return mapPGError(err)
}

func (b *backend) Delete(ctx context.Context, collection, id string) error {
	_, err := b.pool.Exec(ctx, b.getSQLs(collection).delete, id)
	return err
}

func (b *backend) Query(ctx context.Context, collection string, q *den.Query) (den.Iterator, error) {
	sqlStr, args := buildSelectSQL(collection, q)
	rows, err := b.pool.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	return &rowsIterator{rows: rows}, nil
}

func (b *backend) Count(ctx context.Context, collection string, q *den.Query) (int64, error) {
	sqlStr, args := buildCountSQL(collection, q)
	var count int64
	err := b.pool.QueryRow(ctx, sqlStr, args...).Scan(&count)
	return count, err
}

func (b *backend) Exists(ctx context.Context, collection string, q *den.Query) (bool, error) {
	sqlStr, args := buildExistsSQL(collection, q)
	var exists bool
	err := b.pool.QueryRow(ctx, sqlStr, args...).Scan(&exists)
	return exists, err
}

func (b *backend) Aggregate(ctx context.Context, collection string, op den.AggregateOp, field string, q *den.Query) (*float64, error) {
	sqlStr, args, err := buildAggregateSQL(collection, op, field, q)
	if err != nil {
		return nil, err
	}
	var result *float64
	err = b.pool.QueryRow(ctx, sqlStr, args...).Scan(&result)
	return result, err
}

func (b *backend) GroupBy(ctx context.Context, collection string, groupField string, aggs []den.GroupByAgg, q *den.Query) ([]den.GroupByRow, error) {
	sqlStr, args, err := buildGroupBySQL(collection, groupField, aggs, q)
	if err != nil {
		return nil, err
	}
	return scanGroupByRowsPG(ctx, b.pool, sqlStr, args, len(aggs))
}

func (b *backend) EnsureIndex(ctx context.Context, collection string, idx den.IndexDefinition) error {
	return createExpressionIndex(ctx, b.pool, collection, idx)
}

func (b *backend) DropIndex(ctx context.Context, _ string, name string) error {
	_, err := b.pool.Exec(ctx, fmt.Sprintf("DROP INDEX IF EXISTS %s", quoteIdent(name)))
	return err
}

func (b *backend) EnsureCollection(ctx context.Context, name string, _ den.CollectionMeta) error {
	return createTable(ctx, b.pool, name)
}

func (b *backend) DropCollection(ctx context.Context, name string) error {
	_, err := b.pool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", quoteIdent(name)))
	return err
}

func (b *backend) Begin(ctx context.Context, _ bool) (den.Transaction, error) {
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &transaction{tx: tx, ctx: ctx, parent: b}, nil
}

func (b *backend) Ping(ctx context.Context) error {
	return b.pool.Ping(ctx)
}

func (b *backend) Close() error {
	b.pool.Close()
	return nil
}

// pgQuerier abstracts pgxpool.Pool and pgx.Tx for GROUP BY scanning.
type pgQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func scanGroupByRowsPG(ctx context.Context, db pgQuerier, sqlStr string, args []any, numAggs int) ([]den.GroupByRow, error) {
	rows, err := db.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []den.GroupByRow
	for rows.Next() {
		var key *string
		vals := make([]*float64, numAggs)
		scanDest := make([]any, 1+numAggs)
		scanDest[0] = &key
		for i := range numAggs {
			scanDest[i+1] = &vals[i]
		}
		if err := rows.Scan(scanDest...); err != nil {
			return nil, err
		}
		row := den.GroupByRow{Values: make([]float64, numAggs)}
		if key != nil {
			row.Key = *key
		}
		for i, v := range vals {
			if v != nil {
				row.Values[i] = *v
			}
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func mapPGError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return fmt.Errorf("%w: %w", den.ErrDuplicate, err)
	}
	return err
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
