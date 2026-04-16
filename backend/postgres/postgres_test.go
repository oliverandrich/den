package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/where"
)

func connString(t *testing.T) string {
	t.Helper()
	url := os.Getenv("DEN_POSTGRES_URL")
	if url == "" {
		url = "postgres://localhost/den_test"
	}
	return url
}

func openTestDB(t *testing.T) den.Backend {
	t.Helper()
	b, err := Open(connString(t))
	require.NoError(t, err)
	t.Cleanup(func() { b.Close() })
	return b
}

func TestOpen(t *testing.T) {
	url := connString(t)
	b, err := Open(url)
	require.NoError(t, err)
	assert.NotNil(t, b)
	b.Close()
}

func TestOpen_InvalidURL(t *testing.T) {
	_, err := Open("postgres://invalid:5432/nope?connect_timeout=1")
	// pgxpool.New may not fail immediately for bad hosts, but we still exercise the path.
	// If it does succeed, just close it.
	if err == nil {
		t.Log("Open did not fail for invalid URL (lazy connect); that is acceptable")
	}
}

func TestCheckVersion(t *testing.T) {
	b := openTestDB(t)
	pg := b.(*backend)

	ver, err := serverVersion(context.Background(), pg.pool)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, ver, minPGVersion, "local PostgreSQL must be >= %d", minPGVersion)
}

func TestCheckVersion_TooOld(t *testing.T) {
	err := checkMinVersion(120004)
	require.ErrorContains(t, err, "den requires PostgreSQL 13 or later")
	require.ErrorContains(t, err, "got 12.4")
}

func TestCheckVersion_Minimum(t *testing.T) {
	assert.NoError(t, checkMinVersion(130000))
}

func TestCheckVersion_Newer(t *testing.T) {
	assert.NoError(t, checkMinVersion(160002))
}

func TestParseVersionNum(t *testing.T) {
	tests := []struct {
		num        int
		wantMajor  int
		wantMinor  int
		wantString string
	}{
		{130005, 13, 5, "13.5"},
		{160002, 16, 2, "16.2"},
		{120004, 12, 4, "12.4"},
		{100023, 10, 23, "10.23"},
	}
	for _, tt := range tests {
		major, minor := parseVersionNum(tt.num)
		assert.Equal(t, tt.wantMajor, major)
		assert.Equal(t, tt.wantMinor, minor)
		assert.Equal(t, tt.wantString, formatVersion(major, minor))
	}
}

func TestGetNotFound(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_gnf", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_gnf") })

	_, err := b.Get(ctx, "test_gnf", "nonexistent")
	assert.ErrorIs(t, err, den.ErrNotFound)
}

func TestGetPutDelete(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_products", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_products") })

	err := b.Put(ctx, "test_products", "p1", []byte(`{"name":"Widget"}`))
	require.NoError(t, err)

	data, err := b.Get(ctx, "test_products", "p1")
	require.NoError(t, err)
	assert.Contains(t, string(data), "Widget")

	err = b.Delete(ctx, "test_products", "p1")
	require.NoError(t, err)

	_, err = b.Get(ctx, "test_products", "p1")
	assert.ErrorIs(t, err, den.ErrNotFound)
}

func TestQuery_WithCondition(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_query", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_query") })

	require.NoError(t, b.Put(ctx, "test_query", "p1", []byte(`{"name":"Alpha","price":10}`)))
	require.NoError(t, b.Put(ctx, "test_query", "p2", []byte(`{"name":"Beta","price":20}`)))

	q := &den.Query{
		Collection: "test_query",
		Conditions: []where.Condition{where.Field("name").Eq("Beta")},
	}

	iter, err := b.Query(ctx, "test_query", q)
	require.NoError(t, err)
	defer iter.Close()

	var ids []string
	for iter.Next() {
		ids = append(ids, iter.ID())
	}
	assert.Equal(t, []string{"p2"}, ids)
}

func TestPing(t *testing.T) {
	b := openTestDB(t)
	assert.NoError(t, b.Ping(context.Background()))
}

func TestEncoder(t *testing.T) {
	b := openTestDB(t)
	assert.NotNil(t, b.Encoder())
}

func TestDropIndex(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, b.EnsureCollection(ctx, "test_dropidx", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_dropidx") })

	require.NoError(t, b.EnsureIndex(ctx, "test_dropidx", den.IndexDefinition{
		Name: "idx_test_name", Fields: []string{"name"},
	}))
	assert.NoError(t, b.DropIndex(ctx, "test_dropidx", "idx_test_name"))
}

func TestEnsureIndex_Unique(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, b.EnsureCollection(ctx, "test_uniq", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_uniq") })

	require.NoError(t, b.EnsureIndex(ctx, "test_uniq", den.IndexDefinition{
		Name: "idx_test_sku", Fields: []string{"sku"}, Unique: true,
	}))

	require.NoError(t, b.Put(ctx, "test_uniq", "p1", []byte(`{"sku":"ABC"}`)))
	err := b.Put(ctx, "test_uniq", "p2", []byte(`{"sku":"ABC"}`))
	assert.ErrorIs(t, err, den.ErrDuplicate)
}

func TestQuery_NoConditions(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_qnc", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_qnc") })

	require.NoError(t, b.Put(ctx, "test_qnc", "p1", []byte(`{"name":"Alpha","price":10}`)))
	require.NoError(t, b.Put(ctx, "test_qnc", "p2", []byte(`{"name":"Beta","price":20}`)))

	iter, err := b.Query(ctx, "test_qnc", &den.Query{Collection: "test_qnc"})
	require.NoError(t, err)
	defer iter.Close()

	count := 0
	for iter.Next() {
		count++
		assert.NotEmpty(t, iter.ID())
		assert.NotEmpty(t, iter.Bytes())
	}
	assert.Equal(t, 2, count)
	assert.NoError(t, iter.Err())
}

func TestQuery_Sort(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_qsort", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_qsort") })

	require.NoError(t, b.Put(ctx, "test_qsort", "p1", []byte(`{"name":"Alpha","price":30}`)))
	require.NoError(t, b.Put(ctx, "test_qsort", "p2", []byte(`{"name":"Beta","price":10}`)))
	require.NoError(t, b.Put(ctx, "test_qsort", "p3", []byte(`{"name":"Gamma","price":20}`)))

	q := &den.Query{
		Collection: "test_qsort",
		SortFields: []den.SortEntry{{Field: "price", Dir: den.Asc}},
	}

	iter, err := b.Query(ctx, "test_qsort", q)
	require.NoError(t, err)
	defer iter.Close()

	var ids []string
	for iter.Next() {
		ids = append(ids, iter.ID())
	}
	assert.Equal(t, []string{"p2", "p3", "p1"}, ids)
}

func TestQuery_LimitSkip(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_qls", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_qls") })

	require.NoError(t, b.Put(ctx, "test_qls", "p1", []byte(`{"price":10}`)))
	require.NoError(t, b.Put(ctx, "test_qls", "p2", []byte(`{"price":20}`)))
	require.NoError(t, b.Put(ctx, "test_qls", "p3", []byte(`{"price":30}`)))

	q := &den.Query{
		Collection: "test_qls",
		SortFields: []den.SortEntry{{Field: "price", Dir: den.Asc}},
		SkipN:      1,
		LimitN:     1,
	}

	iter, err := b.Query(ctx, "test_qls", q)
	require.NoError(t, err)
	defer iter.Close()

	var ids []string
	for iter.Next() {
		ids = append(ids, iter.ID())
	}
	assert.Equal(t, []string{"p2"}, ids)
}

func TestEnsureIndex(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_idx", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_idx") })

	err := b.EnsureIndex(ctx, "test_idx", den.IndexDefinition{
		Name: "idx_test_price", Fields: []string{"price"},
	})
	assert.NoError(t, err)
}

func TestEnsureIndex_RecordsMetadata(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_idx_meta", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_idx_meta") })

	require.NoError(t, b.EnsureIndex(ctx, "test_idx_meta", den.IndexDefinition{
		Name: "idx_test_meta_name", Fields: []string{"name"},
	}))
	require.NoError(t, b.EnsureIndex(ctx, "test_idx_meta", den.IndexDefinition{
		Name: "idx_test_meta_sku", Fields: []string{"sku"}, Unique: true,
	}))

	recorded, err := b.ListRecordedIndexes(ctx, "test_idx_meta")
	require.NoError(t, err)
	require.Len(t, recorded, 2)
	assert.Equal(t, "idx_test_meta_name", recorded[0].Name)
	assert.Equal(t, []string{"name"}, recorded[0].Fields)
	assert.False(t, recorded[0].Unique)
	assert.Equal(t, "idx_test_meta_sku", recorded[1].Name)
	assert.True(t, recorded[1].Unique)
}

func TestDropIndex_ForgetsMetadata(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_idx_forget", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_idx_forget") })

	require.NoError(t, b.EnsureIndex(ctx, "test_idx_forget", den.IndexDefinition{
		Name: "idx_test_forget_name", Fields: []string{"name"},
	}))
	require.NoError(t, b.DropIndex(ctx, "test_idx_forget", "idx_test_forget_name"))

	recorded, err := b.ListRecordedIndexes(ctx, "test_idx_forget")
	require.NoError(t, err)
	assert.Empty(t, recorded)
}

func TestListRecordedIndexes_ExcludesGIN(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	// EnsureCollection creates the GIN index automatically — it must not be
	// tracked in the metadata table (otherwise DropStaleIndexes could drop it).
	require.NoError(t, b.EnsureCollection(ctx, "test_idx_gin", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_idx_gin") })

	recorded, err := b.ListRecordedIndexes(ctx, "test_idx_gin")
	require.NoError(t, err)
	assert.Empty(t, recorded)
}

func TestEnsureIndex_RecoversInvalid(t *testing.T) {
	b := openTestDB(t)
	pg, ok := b.(*backend)
	require.True(t, ok)
	ctx := context.Background()

	collection := "test_idx_recover"
	indexName := "idx_test_recover_field"

	require.NoError(t, b.EnsureCollection(ctx, collection, den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, collection) })

	require.NoError(t, b.EnsureIndex(ctx, collection, den.IndexDefinition{
		Name: indexName, Fields: []string{"field"},
	}))

	_, err := pg.pool.Exec(ctx,
		`UPDATE pg_index SET indisvalid = false WHERE indexrelid = $1::regclass`,
		indexName)
	if err != nil {
		t.Skipf("cannot mark index invalid (requires superuser): %v", err)
	}

	var valid bool
	require.NoError(t, pg.pool.QueryRow(ctx,
		`SELECT indisvalid FROM pg_index WHERE indexrelid = $1::regclass`,
		indexName).Scan(&valid))
	require.False(t, valid, "precondition: index should be invalid")

	require.NoError(t, b.EnsureIndex(ctx, collection, den.IndexDefinition{
		Name: indexName, Fields: []string{"field"},
	}))

	require.NoError(t, pg.pool.QueryRow(ctx,
		`SELECT indisvalid FROM pg_index WHERE indexrelid = $1::regclass`,
		indexName).Scan(&valid))
	assert.True(t, valid, "index should be valid after EnsureIndex recovers it")
}

func TestQuery_BothCursors(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_cur", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_cur") })

	require.NoError(t, b.Put(ctx, "test_cur", "a1", []byte(`{"name":"A"}`)))
	require.NoError(t, b.Put(ctx, "test_cur", "a2", []byte(`{"name":"B"}`)))
	require.NoError(t, b.Put(ctx, "test_cur", "a3", []byte(`{"name":"C"}`)))

	q := &den.Query{Collection: "test_cur", AfterID: "a1", BeforeID: "a3"}
	iter, err := b.Query(ctx, "test_cur", q)
	require.NoError(t, err)
	defer iter.Close()

	var ids []string
	for iter.Next() {
		ids = append(ids, iter.ID())
	}
	require.NoError(t, iter.Err())
	assert.Equal(t, []string{"a2"}, ids)
}

func TestQuery_CursorWithConditions(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_curc", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_curc") })

	require.NoError(t, b.Put(ctx, "test_curc", "a1", []byte(`{"price":10}`)))
	require.NoError(t, b.Put(ctx, "test_curc", "a2", []byte(`{"price":20}`)))
	require.NoError(t, b.Put(ctx, "test_curc", "a3", []byte(`{"price":30}`)))

	q := &den.Query{
		Collection: "test_curc",
		AfterID:    "a1",
		Conditions: []where.Condition{where.Field("price").Gt(15.0)},
	}
	iter, err := b.Query(ctx, "test_curc", q)
	require.NoError(t, err)
	defer iter.Close()

	var ids []string
	for iter.Next() {
		ids = append(ids, iter.ID())
	}
	require.NoError(t, iter.Err())
	assert.Equal(t, []string{"a2", "a3"}, ids)
}

func TestDropCollection(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_dropcoll", den.CollectionMeta{}))
	require.NoError(t, b.Put(ctx, "test_dropcoll", "p1", []byte(`{"name":"Widget"}`)))
	require.NoError(t, b.DropCollection(ctx, "test_dropcoll"))

	_, err := b.Get(ctx, "test_dropcoll", "p1")
	assert.Error(t, err)
}

func TestCount(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_count", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_count") })

	require.NoError(t, b.Put(ctx, "test_count", "p1", []byte(`{"name":"Alpha","price":10}`)))
	require.NoError(t, b.Put(ctx, "test_count", "p2", []byte(`{"name":"Beta","price":20}`)))
	require.NoError(t, b.Put(ctx, "test_count", "p3", []byte(`{"name":"Alpha","price":30}`)))

	count, err := b.Count(ctx, "test_count", &den.Query{Collection: "test_count"})
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)

	count, err = b.Count(ctx, "test_count", &den.Query{
		Collection: "test_count",
		Conditions: []where.Condition{where.Field("name").Eq("Alpha")},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestExists(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_exists", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_exists") })

	require.NoError(t, b.Put(ctx, "test_exists", "p1", []byte(`{"name":"Alpha"}`)))

	exists, err := b.Exists(ctx, "test_exists", &den.Query{
		Collection: "test_exists",
		Conditions: []where.Condition{where.Field("name").Eq("Alpha")},
	})
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = b.Exists(ctx, "test_exists", &den.Query{
		Collection: "test_exists",
		Conditions: []where.Condition{where.Field("name").Eq("Nonexistent")},
	})
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestAggregate(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_agg", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_agg") })

	require.NoError(t, b.Put(ctx, "test_agg", "p1", []byte(`{"price":10}`)))
	require.NoError(t, b.Put(ctx, "test_agg", "p2", []byte(`{"price":20}`)))
	require.NoError(t, b.Put(ctx, "test_agg", "p3", []byte(`{"price":30}`)))

	// SUM
	result, err := b.Aggregate(ctx, "test_agg", den.OpSum, "price", &den.Query{Collection: "test_agg"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 60.0, *result, 0.01)

	// AVG
	result, err = b.Aggregate(ctx, "test_agg", den.OpAvg, "price", &den.Query{Collection: "test_agg"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 20.0, *result, 0.01)

	// MIN
	result, err = b.Aggregate(ctx, "test_agg", den.OpMin, "price", &den.Query{Collection: "test_agg"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 10.0, *result, 0.01)

	// MAX
	result, err = b.Aggregate(ctx, "test_agg", den.OpMax, "price", &den.Query{Collection: "test_agg"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 30.0, *result, 0.01)

	// Aggregate on empty result set
	result, err = b.Aggregate(ctx, "test_agg", den.OpSum, "price", &den.Query{
		Collection: "test_agg",
		Conditions: []where.Condition{where.Field("price").Gt(100.0)},
	})
	require.NoError(t, err)
	// SUM of no rows returns nil
	assert.Nil(t, result)
}

func TestTransactionGet(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, b.EnsureCollection(ctx, "test_txget", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_txget") })

	require.NoError(t, b.Put(ctx, "test_txget", "p1", []byte(`{"name":"Widget"}`)))

	tx, err := b.Begin(ctx, false)
	require.NoError(t, err)
	defer tx.Rollback()

	data, err := tx.Get(ctx, "test_txget", "p1")
	require.NoError(t, err)
	assert.Contains(t, string(data), "Widget")
}

func TestTransactionCount(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, b.EnsureCollection(ctx, "test_txcnt", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_txcnt") })

	require.NoError(t, b.Put(ctx, "test_txcnt", "p1", []byte(`{"name":"A"}`)))
	require.NoError(t, b.Put(ctx, "test_txcnt", "p2", []byte(`{"name":"B"}`)))

	tx, err := b.Begin(ctx, false)
	require.NoError(t, err)
	defer tx.Rollback()

	count, err := tx.Count(ctx, "test_txcnt", &den.Query{Collection: "test_txcnt"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestTransactionExists(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, b.EnsureCollection(ctx, "test_txex", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_txex") })

	require.NoError(t, b.Put(ctx, "test_txex", "p1", []byte(`{"name":"Alpha"}`)))

	tx, err := b.Begin(ctx, false)
	require.NoError(t, err)
	defer tx.Rollback()

	exists, err := tx.Exists(ctx, "test_txex", &den.Query{
		Collection: "test_txex",
		Conditions: []where.Condition{where.Field("name").Eq("Alpha")},
	})
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = tx.Exists(ctx, "test_txex", &den.Query{
		Collection: "test_txex",
		Conditions: []where.Condition{where.Field("name").Eq("Nope")},
	})
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestTransactionAggregate(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, b.EnsureCollection(ctx, "test_txagg", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_txagg") })

	require.NoError(t, b.Put(ctx, "test_txagg", "p1", []byte(`{"price":10}`)))
	require.NoError(t, b.Put(ctx, "test_txagg", "p2", []byte(`{"price":20}`)))

	tx, err := b.Begin(ctx, false)
	require.NoError(t, err)
	defer tx.Rollback()

	result, err := tx.Aggregate(ctx, "test_txagg", den.OpSum, "price", &den.Query{Collection: "test_txagg"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 30.0, *result, 0.01)
}

func TestTransactionQuery(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, b.EnsureCollection(ctx, "test_txq", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_txq") })

	require.NoError(t, b.Put(ctx, "test_txq", "p1", []byte(`{"name":"Widget"}`)))

	tx, err := b.Begin(ctx, false)
	require.NoError(t, err)
	defer tx.Rollback()

	iter, err := tx.Query(ctx, "test_txq", &den.Query{Collection: "test_txq"})
	require.NoError(t, err)
	defer iter.Close()

	assert.True(t, iter.Next())
	assert.Equal(t, "p1", iter.ID())
	assert.NotEmpty(t, iter.Bytes())
	assert.NoError(t, iter.Err())
}

func TestTransactionDelete(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, b.EnsureCollection(ctx, "test_txdel", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_txdel") })

	require.NoError(t, b.Put(ctx, "test_txdel", "p1", []byte(`{"name":"Del"}`)))

	tx, err := b.Begin(ctx, true)
	require.NoError(t, err)
	require.NoError(t, tx.Delete(ctx, "test_txdel", "p1"))
	require.NoError(t, tx.Commit())

	_, err = b.Get(ctx, "test_txdel", "p1")
	assert.ErrorIs(t, err, den.ErrNotFound)
}

func TestTransactionGetNotFound(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, b.EnsureCollection(ctx, "test_txnf", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_txnf") })

	tx, err := b.Begin(ctx, false)
	require.NoError(t, err)
	defer tx.Rollback()

	_, err = tx.Get(ctx, "test_txnf", "nope")
	assert.ErrorIs(t, err, den.ErrNotFound)
}

func TestTransactionCommitRollback(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_tx", den.CollectionMeta{}))
	t.Cleanup(func() { b.DropCollection(ctx, "test_tx") })

	// Commit
	tx, err := b.Begin(ctx, true)
	require.NoError(t, err)
	require.NoError(t, tx.Put(ctx, "test_tx", "p1", []byte(`{"name":"Committed"}`)))
	require.NoError(t, tx.Commit())

	data, err := b.Get(ctx, "test_tx", "p1")
	require.NoError(t, err)
	assert.Contains(t, string(data), "Committed")

	// Rollback
	tx, err = b.Begin(ctx, true)
	require.NoError(t, err)
	require.NoError(t, tx.Put(ctx, "test_tx", "p2", []byte(`{"name":"Rolled"}`)))
	require.NoError(t, tx.Rollback())

	_, err = b.Get(ctx, "test_tx", "p2")
	assert.ErrorIs(t, err, den.ErrNotFound)
}
