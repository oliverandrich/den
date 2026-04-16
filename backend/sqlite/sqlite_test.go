package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/where"
)

func TestBuildDSN_Defaults(t *testing.T) {
	dsn := buildDSN("/tmp/test.db")
	assert.Contains(t, dsn, "/tmp/test.db?")
	assert.Contains(t, dsn, "_txlock=immediate")
	assert.Contains(t, dsn, "_pragma=journal_mode%28WAL%29")
	assert.Contains(t, dsn, "_pragma=busy_timeout%285000%29")
	assert.Contains(t, dsn, "_pragma=synchronous%28NORMAL%29")
	assert.Contains(t, dsn, "_pragma=foreign_keys%28ON%29")
	assert.Contains(t, dsn, "_pragma=temp_store%28MEMORY%29")
	assert.Contains(t, dsn, "_pragma=mmap_size%28134217728%29")
	assert.Contains(t, dsn, "_pragma=journal_size_limit%2827103364%29")
	assert.Contains(t, dsn, "_pragma=cache_size%282000%29")
}

func TestBuildDSN_UserOverride(t *testing.T) {
	dsn := buildDSN("/tmp/test.db?_pragma=cache_size(5000)")
	// User's cache_size should be present
	assert.Contains(t, dsn, "_pragma=cache_size%285000%29")
	// Default cache_size(2000) should NOT be added
	assert.NotContains(t, dsn, "cache_size%282000%29")
	// Other defaults should still be present
	assert.Contains(t, dsn, "_pragma=journal_mode%28WAL%29")
}

func TestBuildDSN_UserOverrideTxLock(t *testing.T) {
	dsn := buildDSN("/tmp/test.db?_txlock=deferred")
	assert.Contains(t, dsn, "_txlock=deferred")
	assert.NotContains(t, dsn, "_txlock=immediate")
}

func TestBuildDSN_NoDoubleQuestionMark(t *testing.T) {
	dsn := buildDSN("/tmp/test.db?_pragma=cache_size(5000)")
	assert.Equal(t, 1, strings.Count(dsn, "?"), "DSN should have exactly one ?")
}

func TestBuildDSN_PlainPath(t *testing.T) {
	dsn := buildDSN("/tmp/test.db")
	assert.Equal(t, 1, strings.Count(dsn, "?"), "DSN should have exactly one ?")
}

func openTestDB(t *testing.T) den.Backend {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	b, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { b.Close() })
	return b
}

func TestGetPutDelete(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{Name: "products"}))

	err := b.Put(ctx, "products", "p1", []byte(`{"name":"Widget"}`))
	require.NoError(t, err)

	data, err := b.Get(ctx, "products", "p1")
	require.NoError(t, err)
	assert.Contains(t, string(data), "Widget")

	err = b.Delete(ctx, "products", "p1")
	require.NoError(t, err)

	_, err = b.Get(ctx, "products", "p1")
	assert.ErrorIs(t, err, den.ErrNotFound)
}

func TestGetNotFound(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{Name: "products"}))

	_, err := b.Get(ctx, "products", "nonexistent")
	assert.ErrorIs(t, err, den.ErrNotFound)
}

func TestPing(t *testing.T) {
	b := openTestDB(t)
	assert.NoError(t, b.Ping(context.Background()))
}

func TestQuery_NoConditions(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Alpha","price":10}`)))
	require.NoError(t, b.Put(ctx, "products", "p2", []byte(`{"name":"Beta","price":20}`)))

	iter, err := b.Query(ctx, "products", &den.Query{Collection: "products"})
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

func TestQuery_WithCondition(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Alpha","price":10}`)))
	require.NoError(t, b.Put(ctx, "products", "p2", []byte(`{"name":"Beta","price":20}`)))

	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("price").Gt(15.0)},
	}

	iter, err := b.Query(ctx, "products", q)
	require.NoError(t, err)
	defer iter.Close()

	var ids []string
	for iter.Next() {
		ids = append(ids, iter.ID())
	}
	assert.Equal(t, []string{"p2"}, ids)
}

func TestQuery_Sort(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Alpha","price":30}`)))
	require.NoError(t, b.Put(ctx, "products", "p2", []byte(`{"name":"Beta","price":10}`)))
	require.NoError(t, b.Put(ctx, "products", "p3", []byte(`{"name":"Gamma","price":20}`)))

	q := &den.Query{
		Collection: "products",
		SortFields: []den.SortEntry{{Field: "price", Dir: den.Asc}},
	}

	iter, err := b.Query(ctx, "products", q)
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

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"price":10}`)))
	require.NoError(t, b.Put(ctx, "products", "p2", []byte(`{"price":20}`)))
	require.NoError(t, b.Put(ctx, "products", "p3", []byte(`{"price":30}`)))

	q := &den.Query{
		Collection: "products",
		SortFields: []den.SortEntry{{Field: "price", Dir: den.Asc}},
		SkipN:      1,
		LimitN:     1,
	}

	iter, err := b.Query(ctx, "products", q)
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

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))

	err := b.EnsureIndex(ctx, "products", den.IndexDefinition{
		Name: "idx_products_price", Fields: []string{"price"},
	})
	assert.NoError(t, err)
}

func TestEnsureIndex_RecordsMetadata(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))
	require.NoError(t, b.EnsureIndex(ctx, "products", den.IndexDefinition{
		Name: "idx_products_name", Fields: []string{"name"},
	}))
	require.NoError(t, b.EnsureIndex(ctx, "products", den.IndexDefinition{
		Name: "idx_products_sku", Fields: []string{"sku"}, Unique: true,
	}))

	recorded, err := b.ListRecordedIndexes(ctx, "products")
	require.NoError(t, err)
	require.Len(t, recorded, 2)
	assert.Equal(t, "idx_products_name", recorded[0].Name)
	assert.Equal(t, []string{"name"}, recorded[0].Fields)
	assert.False(t, recorded[0].Unique)
	assert.Equal(t, "idx_products_sku", recorded[1].Name)
	assert.Equal(t, []string{"sku"}, recorded[1].Fields)
	assert.True(t, recorded[1].Unique)
}

func TestDropIndex_ForgetsMetadata(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))
	require.NoError(t, b.EnsureIndex(ctx, "products", den.IndexDefinition{
		Name: "idx_products_name", Fields: []string{"name"},
	}))
	require.NoError(t, b.DropIndex(ctx, "products", "idx_products_name"))

	recorded, err := b.ListRecordedIndexes(ctx, "products")
	require.NoError(t, err)
	assert.Empty(t, recorded)
}

func TestListRecordedIndexes_IsolatedByCollection(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))
	require.NoError(t, b.EnsureCollection(ctx, "orders", den.CollectionMeta{}))
	require.NoError(t, b.EnsureIndex(ctx, "products", den.IndexDefinition{
		Name: "idx_products_sku", Fields: []string{"sku"},
	}))
	require.NoError(t, b.EnsureIndex(ctx, "orders", den.IndexDefinition{
		Name: "idx_orders_customer", Fields: []string{"customer"},
	}))

	products, err := b.ListRecordedIndexes(ctx, "products")
	require.NoError(t, err)
	require.Len(t, products, 1)
	assert.Equal(t, "idx_products_sku", products[0].Name)

	orders, err := b.ListRecordedIndexes(ctx, "orders")
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "idx_orders_customer", orders[0].Name)
}

func TestEnsureIndex_Unique(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))

	err := b.EnsureIndex(ctx, "products", den.IndexDefinition{
		Name: "idx_products_sku", Fields: []string{"sku"}, Unique: true,
	})
	assert.NoError(t, err)

	// Insert two docs with same SKU — should fail
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"sku":"ABC"}`)))
	err = b.Put(ctx, "products", "p2", []byte(`{"sku":"ABC"}`))
	assert.Error(t, err) // unique constraint violation
}

func TestDropCollection(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Widget"}`)))
	require.NoError(t, b.DropCollection(ctx, "products"))

	// Table should be gone
	_, err := b.Get(ctx, "products", "p1")
	assert.Error(t, err)
}

func TestTransactionCommit(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))

	tx, err := b.Begin(ctx, true)
	require.NoError(t, err)

	require.NoError(t, tx.Put(ctx, "products", "p1", []byte(`{"name":"InTx"}`)))
	require.NoError(t, tx.Commit())

	data, err := b.Get(ctx, "products", "p1")
	require.NoError(t, err)
	assert.Contains(t, string(data), "InTx")
}

func TestTransactionRollback(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))

	tx, err := b.Begin(ctx, true)
	require.NoError(t, err)

	require.NoError(t, tx.Put(ctx, "products", "p1", []byte(`{"name":"Rollback"}`)))
	require.NoError(t, tx.Rollback())

	_, err = b.Get(ctx, "products", "p1")
	assert.ErrorIs(t, err, den.ErrNotFound)
}

func TestQuery_BothCursors(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "test_cur", den.CollectionMeta{}))
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

func TestDropIndex(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))
	require.NoError(t, b.EnsureIndex(ctx, "products", den.IndexDefinition{
		Name: "idx_products_price", Fields: []string{"price"},
	}))
	assert.NoError(t, b.DropIndex(ctx, "products", "idx_products_price"))
}

func TestTransactionGet(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Widget"}`)))

	tx, err := b.Begin(ctx, false)
	require.NoError(t, err)
	defer tx.Rollback()

	data, err := tx.Get(ctx, "products", "p1")
	require.NoError(t, err)
	assert.Contains(t, string(data), "Widget")
}

func TestTransactionGetNotFound(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))

	tx, err := b.Begin(ctx, false)
	require.NoError(t, err)
	defer tx.Rollback()

	_, err = tx.Get(ctx, "products", "none")
	assert.ErrorIs(t, err, den.ErrNotFound)
}

func TestTransactionDelete(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Widget"}`)))

	tx, err := b.Begin(ctx, true)
	require.NoError(t, err)
	require.NoError(t, tx.Delete(ctx, "products", "p1"))
	require.NoError(t, tx.Commit())

	_, err = b.Get(ctx, "products", "p1")
	assert.ErrorIs(t, err, den.ErrNotFound)
}

func TestTransactionQuery(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Widget"}`)))

	tx, err := b.Begin(ctx, false)
	require.NoError(t, err)
	defer tx.Rollback()

	iter, err := tx.Query(ctx, "products", &den.Query{Collection: "products"})
	require.NoError(t, err)
	defer iter.Close()

	assert.True(t, iter.Next())
	assert.Equal(t, "p1", iter.ID())
}

func TestEncoder(t *testing.T) {
	b := openTestDB(t)
	assert.NotNil(t, b.Encoder())
}

func TestCount(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{Name: "products"}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Alpha","price":10}`)))
	require.NoError(t, b.Put(ctx, "products", "p2", []byte(`{"name":"Beta","price":20}`)))
	require.NoError(t, b.Put(ctx, "products", "p3", []byte(`{"name":"Alpha","price":30}`)))

	count, err := b.Count(ctx, "products", &den.Query{Collection: "products"})
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)

	count, err = b.Count(ctx, "products", &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").Eq("Alpha")},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestAggregate(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{Name: "products"}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Alpha","price":10}`)))
	require.NoError(t, b.Put(ctx, "products", "p2", []byte(`{"name":"Beta","price":20}`)))
	require.NoError(t, b.Put(ctx, "products", "p3", []byte(`{"name":"Gamma","price":30}`)))

	q := &den.Query{Collection: "products"}

	// SUM
	result, err := b.Aggregate(ctx, "products", den.OpSum, "price", q)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 60.0, *result, 0.001)

	// AVG
	result, err = b.Aggregate(ctx, "products", den.OpAvg, "price", q)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 20.0, *result, 0.001)

	// MIN
	result, err = b.Aggregate(ctx, "products", den.OpMin, "price", q)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 10.0, *result, 0.001)

	// MAX
	result, err = b.Aggregate(ctx, "products", den.OpMax, "price", q)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 30.0, *result, 0.001)

	// Aggregate with condition
	qFiltered := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("price").Gt(10.0)},
	}
	result, err = b.Aggregate(ctx, "products", den.OpSum, "price", qFiltered)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 50.0, *result, 0.001)

	// Aggregate on empty result returns nil
	qEmpty := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("price").Gt(100.0)},
	}
	result, err = b.Aggregate(ctx, "products", den.OpSum, "price", qEmpty)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestTransactionCount(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{Name: "products"}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Alpha"}`)))
	require.NoError(t, b.Put(ctx, "products", "p2", []byte(`{"name":"Beta"}`)))

	tx, err := b.Begin(ctx, false)
	require.NoError(t, err)
	defer tx.Rollback()

	count, err := tx.Count(ctx, "products", &den.Query{Collection: "products"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	count, err = tx.Count(ctx, "products", &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").Eq("Alpha")},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestTransactionExists(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{Name: "products"}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Alpha"}`)))

	tx, err := b.Begin(ctx, false)
	require.NoError(t, err)
	defer tx.Rollback()

	exists, err := tx.Exists(ctx, "products", &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").Eq("Alpha")},
	})
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = tx.Exists(ctx, "products", &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").Eq("Nonexistent")},
	})
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestTransactionAggregate(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{Name: "products"}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Alpha","price":10}`)))
	require.NoError(t, b.Put(ctx, "products", "p2", []byte(`{"name":"Beta","price":20}`)))
	require.NoError(t, b.Put(ctx, "products", "p3", []byte(`{"name":"Gamma","price":30}`)))

	tx, err := b.Begin(ctx, false)
	require.NoError(t, err)
	defer tx.Rollback()

	q := &den.Query{Collection: "products"}

	result, err := tx.Aggregate(ctx, "products", den.OpSum, "price", q)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 60.0, *result, 0.001)

	result, err = tx.Aggregate(ctx, "products", den.OpAvg, "price", q)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 20.0, *result, 0.001)
}

func TestRegexp(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{Name: "products"}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Alpha"}`)))
	require.NoError(t, b.Put(ctx, "products", "p2", []byte(`{"name":"Beta"}`)))
	require.NoError(t, b.Put(ctx, "products", "p3", []byte(`{"name":"Apex"}`)))

	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").RegExp("^A")},
	}

	iter, err := b.Query(ctx, "products", q)
	require.NoError(t, err)
	defer iter.Close()

	var ids []string
	for iter.Next() {
		ids = append(ids, iter.ID())
	}
	require.NoError(t, iter.Err())
	assert.Len(t, ids, 2)
	assert.Contains(t, ids, "p1")
	assert.Contains(t, ids, "p3")
}

func TestExists(t *testing.T) {
	b := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, b.EnsureCollection(ctx, "products", den.CollectionMeta{Name: "products"}))
	require.NoError(t, b.Put(ctx, "products", "p1", []byte(`{"name":"Alpha"}`)))

	exists, err := b.Exists(ctx, "products", &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").Eq("Alpha")},
	})
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = b.Exists(ctx, "products", &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").Eq("Nonexistent")},
	})
	require.NoError(t, err)
	assert.False(t, exists)
}
