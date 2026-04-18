package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"modernc.org/sqlite"

	"github.com/oliverandrich/den"
)

const maxRegexpCacheSize = 256

var (
	regexpCacheMu      sync.Mutex
	regexpCacheEntries = make(map[string]*regexp.Regexp)
)

func init() {
	den.RegisterBackend("sqlite", func(ctx context.Context, dsn string) (den.Backend, error) {
		path := dsn
		// Strip sqlite:// prefix if present
		if after, ok := strings.CutPrefix(dsn, "sqlite://"); ok {
			path = strings.TrimPrefix(after, "/")
		}
		return Open(ctx, path)
	})

	sqlite.MustRegisterScalarFunction("regexp", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		pattern, ok1 := args[0].(string)
		value, ok2 := args[1].(string)
		if !ok1 || !ok2 {
			return int64(0), nil
		}
		re, err := getOrCompileRegexp(pattern)
		if err != nil {
			return int64(0), nil //nolint:nilerr // invalid regex pattern should return no-match, not error
		}
		if re.MatchString(value) {
			return int64(1), nil
		}
		return int64(0), nil
	})
}

func getOrCompileRegexp(pattern string) (*regexp.Regexp, error) {
	regexpCacheMu.Lock()
	defer regexpCacheMu.Unlock()

	if re, ok := regexpCacheEntries[pattern]; ok {
		return re, nil
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	if len(regexpCacheEntries) >= maxRegexpCacheSize {
		// Evict all — simple and effective since patterns rarely change
		regexpCacheEntries = make(map[string]*regexp.Regexp)
	}

	regexpCacheEntries[pattern] = re
	return re, nil
}

type stmtSet struct {
	get    *sql.Stmt
	put    *sql.Stmt
	delete *sql.Stmt
}

type backend struct {
	db    *sql.DB
	stmts sync.Map // collection name → *stmtSet
}

// Open opens a SQLite database at the given path. The context governs the
// initial metadata-table creation so callers with a startup deadline can
// cancel cleanly.
// The path may include query parameters to override default PRAGMAs,
// e.g. "/data.db?_pragma=cache_size(5000)".
func Open(ctx context.Context, path string) (den.Backend, error) {
	dsn := buildDSN(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite open %q: %w", path, err)
	}
	if err := ensureMetadataTable(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite open %q: %w", path, err)
	}
	return &backend{db: db}, nil
}

// defaultPragmas are applied unless the user overrides them in the DSN.
var defaultPragmas = []string{
	"journal_mode(WAL)",
	"busy_timeout(5000)",
	"synchronous(NORMAL)",
	"foreign_keys(ON)",
	"temp_store(MEMORY)",
	"mmap_size(134217728)",
	"journal_size_limit(27103364)",
	"cache_size(2000)",
}

// buildDSN constructs a SQLite DSN with sensible defaults.
// User-provided query parameters (e.g. "?_pragma=cache_size(5000)")
// take precedence over defaults.
func buildDSN(path string) string {
	// Split path and query string
	base, queryStr, _ := strings.Cut(path, "?")

	userParams, _ := url.ParseQuery(queryStr)

	// Collect user-supplied pragma names for override detection
	userPragmas := make(map[string]bool)
	for _, p := range userParams["_pragma"] {
		name, _, _ := strings.Cut(p, "(")
		userPragmas[strings.ToLower(name)] = true
	}

	// Build final query params: start with user params
	params := make(url.Values)
	maps.Copy(params, userParams)

	// Set _txlock default if not provided
	if params.Get("_txlock") == "" {
		params.Set("_txlock", "immediate")
	}

	// Add default pragmas that the user hasn't overridden
	for _, pragma := range defaultPragmas {
		name, _, _ := strings.Cut(pragma, "(")
		if !userPragmas[strings.ToLower(name)] {
			params.Add("_pragma", pragma)
		}
	}

	return base + "?" + params.Encode()
}

func (b *backend) Encoder() den.Encoder {
	return newJSONEncoder()
}

func (b *backend) getStmts(ctx context.Context, collection string) (*stmtSet, error) {
	if v, ok := b.stmts.Load(collection); ok {
		s, _ := v.(*stmtSet)
		return s, nil
	}

	set := &stmtSet{}
	var err error
	set.get, err = b.db.PrepareContext(ctx, fmt.Sprintf("SELECT json(data) FROM %q WHERE id = ?", collection))
	if err != nil {
		return nil, err
	}
	set.put, err = b.db.PrepareContext(ctx, fmt.Sprintf("INSERT INTO %q (id, data) VALUES (?, jsonb(?)) ON CONFLICT(id) DO UPDATE SET data = jsonb(?)", collection))
	if err != nil {
		_ = set.get.Close()
		return nil, err
	}
	set.delete, err = b.db.PrepareContext(ctx, fmt.Sprintf("DELETE FROM %q WHERE id = ?", collection))
	if err != nil {
		_ = set.get.Close()
		_ = set.put.Close()
		return nil, err
	}

	if actual, loaded := b.stmts.LoadOrStore(collection, set); loaded {
		_ = set.get.Close()
		_ = set.put.Close()
		_ = set.delete.Close()
		s, _ := actual.(*stmtSet)
		return s, nil
	}
	return set, nil
}

func (b *backend) Get(ctx context.Context, collection, id string) ([]byte, error) {
	stmts, err := b.getStmts(ctx, collection)
	if err != nil {
		return nil, err
	}
	var data []byte
	err = stmts.get.QueryRowContext(ctx, id).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, den.ErrNotFound
		}
		return nil, err
	}
	return data, nil
}

func (b *backend) Put(ctx context.Context, collection, id string, data []byte) error {
	stmts, err := b.getStmts(ctx, collection)
	if err != nil {
		return err
	}
	_, err = stmts.put.ExecContext(ctx, id, data, data)
	return mapSQLiteError(err)
}

func (b *backend) Delete(ctx context.Context, collection, id string) error {
	stmts, err := b.getStmts(ctx, collection)
	if err != nil {
		return err
	}
	_, err = stmts.delete.ExecContext(ctx, id)
	return err
}

func (b *backend) Query(ctx context.Context, collection string, q *den.Query) (den.Iterator, error) {
	sqlStr, args := buildSelectSQL(collection, q)
	rows, err := b.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	return &rowsIterator{rows: rows}, nil
}

func (b *backend) Count(ctx context.Context, collection string, q *den.Query) (int64, error) {
	sqlStr, args := buildCountSQL(collection, q)
	var count int64
	err := b.db.QueryRowContext(ctx, sqlStr, args...).Scan(&count)
	return count, err
}

func (b *backend) Exists(ctx context.Context, collection string, q *den.Query) (bool, error) {
	sqlStr, args := buildExistsSQL(collection, q)
	var exists bool
	err := b.db.QueryRowContext(ctx, sqlStr, args...).Scan(&exists)
	return exists, err
}

func (b *backend) Aggregate(ctx context.Context, collection string, op den.AggregateOp, field string, q *den.Query) (*float64, error) {
	sqlStr, args, err := buildAggregateSQL(collection, op, field, q)
	if err != nil {
		return nil, err
	}
	var result *float64
	err = b.db.QueryRowContext(ctx, sqlStr, args...).Scan(&result)
	return result, err
}

func (b *backend) GroupBy(ctx context.Context, collection string, groupField string, aggs []den.GroupByAgg, q *den.Query) ([]den.GroupByRow, error) {
	sqlStr, args, err := buildGroupBySQL(collection, groupField, aggs, q)
	if err != nil {
		return nil, err
	}
	return scanGroupByRows(ctx, b.db, sqlStr, args, len(aggs))
}

func (b *backend) EnsureIndex(ctx context.Context, collection string, idx den.IndexDefinition) error {
	return createExpressionIndex(ctx, b.db, collection, idx)
}

func (b *backend) DropIndex(ctx context.Context, _ string, name string) error {
	if _, err := b.db.ExecContext(ctx, fmt.Sprintf("DROP INDEX IF EXISTS %q", name)); err != nil {
		return err
	}
	return forgetIndex(ctx, b.db, name)
}

func (b *backend) ListRecordedIndexes(ctx context.Context, collection string) ([]den.RecordedIndex, error) {
	return listRecordedIndexes(ctx, b.db, collection)
}

func (b *backend) EnsureCollection(ctx context.Context, name string, _ den.CollectionMeta) error {
	return createTable(ctx, b.db, name)
}

func (b *backend) DropCollection(ctx context.Context, name string) error {
	_, err := b.db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %q", name))
	return err
}

func (b *backend) Begin(ctx context.Context, _ bool) (den.Transaction, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &transaction{tx: tx, parent: b}, nil
}

func (b *backend) Ping(ctx context.Context) error {
	return b.db.PingContext(ctx)
}

func (b *backend) Close() error {
	b.stmts.Range(func(_, v any) bool {
		set, _ := v.(*stmtSet)
		_ = set.get.Close()
		_ = set.put.Close()
		_ = set.delete.Close()
		return true
	})
	return b.db.Close()
}

// scanGroupByRows executes a GROUP BY query and scans the results.
// The first column is the group key (text), followed by numAggs float columns.
func scanGroupByRows(ctx context.Context, db interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, sqlStr string, args []any, numAggs int) ([]den.GroupByRow, error) {
	rows, err := db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []den.GroupByRow
	var key *string
	vals := make([]*float64, numAggs)
	scanDest := make([]any, 1+numAggs)
	scanDest[0] = &key
	for i := range numAggs {
		scanDest[i+1] = &vals[i]
	}
	for rows.Next() {
		key = nil
		for i := range vals {
			vals[i] = nil
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

func mapSQLiteError(err error) error {
	if err == nil {
		return nil
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code() == 2067 { // SQLITE_CONSTRAINT_UNIQUE
		return fmt.Errorf("%w: %w", den.ErrDuplicate, err)
	}
	return err
}
