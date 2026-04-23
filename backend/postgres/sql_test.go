package postgres

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/where"
)

func TestBuildSelectSQL_NoConditions(t *testing.T) {
	q := &den.Query{Collection: "products"}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, `SELECT id, data::text FROM "products"`)
	assert.Empty(t, args)
}

func TestBuildSelectSQL_Eq(t *testing.T) {
	// Simple top-level Eq with a scalar value is rewritten to a containment
	// clause so the GIN(data jsonb_path_ops) index can serve the query.
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").Eq("Widget")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "data @> $1::jsonb")
	assert.NotContains(t, sql, "jsonb_extract_path(data, 'name') =")
	assert.Equal(t, []any{[]byte(`{"name":"Widget"}`)}, args)
}

func TestBuildSelectSQL_Eq_RewriteTypes(t *testing.T) {
	tests := []struct {
		name    string
		cond    where.Condition
		wantArg []byte
	}{
		{"string", where.Field("status").Eq("published"), []byte(`{"status":"published"}`)},
		{"int", where.Field("stock").Eq(42), []byte(`{"stock":42}`)},
		{"int64", where.Field("stock").Eq(int64(42)), []byte(`{"stock":42}`)},
		{"float", where.Field("price").Eq(3.5), []byte(`{"price":3.5}`)},
		{"bool", where.Field("active").Eq(true), []byte(`{"active":true}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &den.Query{Collection: "t", Conditions: []where.Condition{tt.cond}}
			sql, args := buildSelectSQL("t", q)
			assert.Contains(t, sql, "data @> $1::jsonb")
			assert.Equal(t, []any{tt.wantArg}, args)
		})
	}
}

func TestBuildSelectSQL_Eq_NoRewrite(t *testing.T) {
	// Cases where Eq must NOT be rewritten to containment — either the
	// semantics differ (@> is subset, not equality) or we can't build a
	// safe top-level JSONB object.
	tests := []struct {
		name string
		cond where.Condition
	}{
		{"nested field", where.Field("address.city").Eq("Berlin")},
		{"slice value", where.Field("tags").Eq([]string{"a", "b"})},
		{"map value", where.Field("metadata").Eq(map[string]any{"k": "v"})},
		{"nil value", where.Field("status").Eq(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &den.Query{Collection: "t", Conditions: []where.Condition{tt.cond}}
			sql, _ := buildSelectSQL("t", q)
			assert.NotContains(t, sql, "data @>")
			assert.Contains(t, sql, "jsonb_extract_path(data,")
		})
	}
}

func TestBuildSelectSQL_Sort(t *testing.T) {
	q := &den.Query{Collection: "products", SortFields: []den.SortEntry{{Field: "price", Dir: den.Asc}}}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "ORDER BY jsonb_extract_path(data, 'price') ASC")
}

func TestBuildSelectSQL_SortDesc(t *testing.T) {
	q := &den.Query{Collection: "products", SortFields: []den.SortEntry{{Field: "price", Dir: den.Desc}}}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "DESC")
}

func TestBuildSelectSQL_LimitSkip(t *testing.T) {
	q := &den.Query{Collection: "products", LimitN: 10, SkipN: 5}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "LIMIT 10")
	assert.Contains(t, sql, "OFFSET 5")
}

func TestBuildSelectSQL_Cursor(t *testing.T) {
	q := &den.Query{Collection: "products", AfterID: "p5"}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "id > $1")
	assert.Contains(t, args, "p5")
}

func TestBuildSelectSQL_BeforeCursor(t *testing.T) {
	q := &den.Query{Collection: "products", BeforeID: "p3"}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "id < $1")
	assert.Contains(t, args, "p3")
}

func TestBuildSelectSQL_Comparison(t *testing.T) {
	tests := []struct {
		name string
		cond where.Condition
		want string
	}{
		{"Ne", where.Field("x").Ne(1), "jsonb_extract_path(data, 'x') != $1::jsonb"},
		{"Gt", where.Field("x").Gt(1), "jsonb_extract_path(data, 'x') > $1::jsonb"},
		{"Gte", where.Field("x").Gte(1), "jsonb_extract_path(data, 'x') >= $1::jsonb"},
		{"Lt", where.Field("x").Lt(1), "jsonb_extract_path(data, 'x') < $1::jsonb"},
		{"Lte", where.Field("x").Lte(1), "jsonb_extract_path(data, 'x') <= $1::jsonb"},
		{"IsNil", where.Field("x").IsNil(), "IS NULL"},
		{"IsNotNil", where.Field("x").IsNotNil(), "IS NOT NULL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &den.Query{Collection: "t", Conditions: []where.Condition{tt.cond}}
			sql, _ := buildSelectSQL("t", q)
			assert.Contains(t, sql, tt.want)
		})
	}
}

func TestBuildSelectSQL_In(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("status").In("a", "b")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "IN ($1::jsonb, $2::jsonb)")
	assert.Equal(t, []any{[]byte(`"a"`), []byte(`"b"`)}, args)
}

func TestBuildSelectSQL_NotIn(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("status").NotIn("x")},
	}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "NOT IN")
}

func TestBuildSelectSQL_Contains(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("tags").Contains("go")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "jsonb_extract_path(data, 'tags') @>")
	assert.Equal(t, []any{"go"}, args)
}

func TestBuildSelectSQL_StartsWith(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").StartsWith("Widget")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, `jsonb_extract_path_text(data, 'name') LIKE $1 ESCAPE '\'`)
	assert.Equal(t, []any{`Widget%`}, args)
}

func TestBuildSelectSQL_EndsWith(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").EndsWith("Widget")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, `LIKE $1 ESCAPE '\'`)
	assert.Equal(t, []any{`%Widget`}, args)
}

func TestBuildSelectSQL_StringContains(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").StringContains("idge")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, `LIKE $1 ESCAPE '\'`)
	assert.Equal(t, []any{`%idge%`}, args)
}

func TestBuildSelectSQL_LikeOps_EscapeSpecialChars(t *testing.T) {
	// Confirms escapeLike fires on every LIKE-based operator — the literal
	// % and _ must be neutralized in the wire arg so a future refactor can't
	// silently bypass escapeLike in one branch and turn a literal match into
	// an accidental wildcard.
	cases := []struct {
		name string
		cond where.Condition
		want any
	}{
		{"StartsWith", where.Field("name").StartsWith(`50%_off`), `50\%\_off%`},
		{"EndsWith", where.Field("name").EndsWith(`50%_off`), `%50\%\_off`},
		{"StringContains", where.Field("name").StringContains(`50%_off`), `%50\%\_off%`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := &den.Query{Collection: "products", Conditions: []where.Condition{tc.cond}}
			_, args := buildSelectSQL("products", q)
			assert.Equal(t, []any{tc.want}, args)
		})
	}
}

func TestBuildSelectSQL_ContainsAny(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("tags").ContainsAny("a", "b")},
	}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "OR")
}

func TestBuildSelectSQL_RegExp(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").RegExp("^Widget")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "~ $1")
	assert.Equal(t, []any{"^Widget"}, args)
}

func TestBuildSelectSQL_FieldRef(t *testing.T) {
	q := &den.Query{
		Collection: "events",
		Conditions: []where.Condition{where.Field("end").Gt(where.FieldRef("start"))},
	}
	sql, args := buildSelectSQL("events", q)
	assert.Contains(t, sql, "jsonb_extract_path(data, 'end') > jsonb_extract_path(data, 'start')")
	assert.Empty(t, args)
}

func TestBuildSelectSQL_HasKey(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("metadata").HasKey("color")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "jsonb_exists(jsonb_extract_path(data, 'metadata')")
	assert.Contains(t, args, "color")
}

func TestBuildSelectSQL_NestedField(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("address.city").Eq("Berlin")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "jsonb_extract_path(data, 'address', 'city') = $1::jsonb")
	assert.Equal(t, []any{[]byte(`"Berlin"`)}, args)
}

func TestBuildSelectSQL_NestedFieldSort(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		SortFields: []den.SortEntry{{Field: "category.name", Dir: den.Asc}},
	}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "ORDER BY jsonb_extract_path(data, 'category', 'name') ASC")
}

func TestBuildSelectSQL_StringGt(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").Gt("A")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "jsonb_extract_path(data, 'name') > $1::jsonb")
	assert.NotContains(t, sql, "::float")
	assert.Equal(t, []any{[]byte(`"A"`)}, args)
}

func TestBuildSelectSQL_ContainsAll(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("tags").ContainsAll("a", "b")},
	}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "AND")
}

func TestBuildSelectSQL_And(t *testing.T) {
	q := &den.Query{
		Collection: "t",
		Conditions: []where.Condition{where.And(where.Field("a").Eq(1), where.Field("b").Eq(2))},
	}
	sql, args := buildSelectSQL("t", q)
	assert.Contains(t, sql, "AND")
	assert.Len(t, args, 2)
}

func TestBuildSelectSQL_Or(t *testing.T) {
	q := &den.Query{
		Collection: "t",
		Conditions: []where.Condition{where.Or(where.Field("a").Eq(1), where.Field("b").Eq(2))},
	}
	sql, _ := buildSelectSQL("t", q)
	assert.Contains(t, sql, "OR")
}

func TestBuildSelectSQL_Not(t *testing.T) {
	q := &den.Query{
		Collection: "t",
		Conditions: []where.Condition{where.Not(where.Field("x").Eq(1))},
	}
	sql, _ := buildSelectSQL("t", q)
	assert.Contains(t, sql, "NOT")
}

func TestBuildSelectSQL_BothCursors(t *testing.T) {
	q := &den.Query{Collection: "products", AfterID: "p1", BeforeID: "p5"}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "id > $1")
	assert.Contains(t, sql, "id < $2")
	assert.Equal(t, []any{"p1", "p5"}, args)
}

func TestBuildSelectSQL_FieldRefComparisons(t *testing.T) {
	tests := []struct {
		name string
		cond where.Condition
		want string
	}{
		{"Eq", where.Field("a").Eq(where.FieldRef("b")), "jsonb_extract_path(data, 'a') = jsonb_extract_path(data, 'b')"},
		{"Ne", where.Field("a").Ne(where.FieldRef("b")), "jsonb_extract_path(data, 'a') != jsonb_extract_path(data, 'b')"},
		{"Gt", where.Field("a").Gt(where.FieldRef("b")), "jsonb_extract_path(data, 'a') > jsonb_extract_path(data, 'b')"},
		{"Gte", where.Field("a").Gte(where.FieldRef("b")), "jsonb_extract_path(data, 'a') >= jsonb_extract_path(data, 'b')"},
		{"Lt", where.Field("a").Lt(where.FieldRef("b")), "jsonb_extract_path(data, 'a') < jsonb_extract_path(data, 'b')"},
		{"Lte", where.Field("a").Lte(where.FieldRef("b")), "jsonb_extract_path(data, 'a') <= jsonb_extract_path(data, 'b')"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &den.Query{Collection: "t", Conditions: []where.Condition{tt.cond}}
			sql, args := buildSelectSQL("t", q)
			assert.Contains(t, sql, tt.want)
			assert.Empty(t, args)
		})
	}
}

func TestBuildCountSQL(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("price").Gt(10)},
	}
	sql, args := buildCountSQL("products", q)
	assert.Contains(t, sql, `SELECT COUNT(*) FROM "products" WHERE`)
	assert.Equal(t, []any{[]byte("10")}, args)
}

func TestBuildCountSQL_NoConditions(t *testing.T) {
	q := &den.Query{Collection: "products"}
	sql, args := buildCountSQL("products", q)
	assert.Equal(t, `SELECT COUNT(*) FROM "products"`, sql)
	assert.Empty(t, args)
}

func TestBuildExistsSQL(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").Eq("Alpha")},
	}
	sql, args := buildExistsSQL("products", q)
	assert.Contains(t, sql, "SELECT EXISTS(")
	assert.Contains(t, sql, "LIMIT 1")
	assert.Contains(t, sql, "data @> $1::jsonb")
	assert.Equal(t, []any{[]byte(`{"name":"Alpha"}`)}, args)
}

func TestBuildAggregateSQL_Sum(t *testing.T) {
	q := &den.Query{Collection: "products"}
	sql, args, err := buildAggregateSQL("products", den.OpSum, "price", q)
	require.NoError(t, err)
	assert.Contains(t, sql, `SUM((jsonb_extract_path_text(data, 'price'))::float)`)
	assert.Contains(t, sql, `FROM "products"`)
	assert.Empty(t, args)
}

func TestBuildAggregateSQL_AvgWithFilter(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("category").Eq("X")},
	}
	sql, args, err := buildAggregateSQL("products", den.OpAvg, "price", q)
	require.NoError(t, err)
	assert.Contains(t, sql, `AVG((jsonb_extract_path_text(data, 'price'))::float)`)
	assert.Contains(t, sql, "WHERE")
	assert.Contains(t, sql, "data @> $1::jsonb")
	assert.Equal(t, []any{[]byte(`{"category":"X"}`)}, args)
}

func TestBuildAggregateSQL_UnsupportedOp(t *testing.T) {
	q := &den.Query{Collection: "products"}
	_, _, err := buildAggregateSQL("products", den.AggregateOp("INVALID"), "price", q)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported aggregate op")
}

func TestMapPGError_Nil(t *testing.T) {
	assert.NoError(t, mapPGError(nil))
}

func TestMapPGError_Codes(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		sentinel error
	}{
		{"unique violation", "23505", den.ErrDuplicate},
		{"lock not available", "55P03", den.ErrLocked},
		{"deadlock detected", "40P01", den.ErrDeadlock},
		{"serialization failure", "40001", den.ErrSerialization},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgErr := &pgconn.PgError{Code: tt.code, Message: "synthesized"}
			got := mapPGError(pgErr)
			require.ErrorIs(t, got, tt.sentinel)
		})
	}
}

func TestMapPGError_UnknownCode(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "99999", Message: "mystery"}
	got := mapPGError(pgErr)
	require.Error(t, got)
	// Unknown codes pass through unwrapped; no sentinel match.
	require.NotErrorIs(t, got, den.ErrDuplicate)
	require.NotErrorIs(t, got, den.ErrLocked)
	require.NotErrorIs(t, got, den.ErrDeadlock)
	require.NotErrorIs(t, got, den.ErrSerialization)
}

func TestQuoteIdent(t *testing.T) {
	assert.Equal(t, `"products"`, quoteIdent("products"))
	assert.Equal(t, `"my""table"`, quoteIdent(`my"table`))
}

// --- buildGroupBySQL tests ---

func TestBuildGroupBySQL_Count(t *testing.T) {
	q := &den.Query{Collection: "products"}
	sql, args, err := buildGroupBySQL("products", []string{"category"},
		[]den.GroupByAgg{{Op: den.OpCount}}, q)
	require.NoError(t, err)
	assert.Contains(t, sql, `SELECT jsonb_extract_path_text(data, 'category'), COUNT(*) FROM "products"`)
	assert.Contains(t, sql, `GROUP BY jsonb_extract_path_text(data, 'category')`)
	assert.Empty(t, args)
}

func TestBuildGroupBySQL_NumericAgg(t *testing.T) {
	q := &den.Query{Collection: "products"}
	sql, _, err := buildGroupBySQL("products", []string{"category"},
		[]den.GroupByAgg{{Op: den.OpSum, Field: "price"}}, q)
	require.NoError(t, err)
	assert.Contains(t, sql, `SUM((jsonb_extract_path_text(data, 'price'))::float)`)
}

func TestBuildGroupBySQL_MultipleAggs(t *testing.T) {
	q := &den.Query{Collection: "products"}
	sql, _, err := buildGroupBySQL("products", []string{"category"}, []den.GroupByAgg{
		{Op: den.OpCount},
		{Op: den.OpSum, Field: "price"},
		{Op: den.OpAvg, Field: "price"},
		{Op: den.OpMin, Field: "price"},
		{Op: den.OpMax, Field: "price"},
	}, q)
	require.NoError(t, err)
	positions := []int{
		strings.Index(sql, "COUNT(*)"),
		strings.Index(sql, "SUM("),
		strings.Index(sql, "AVG("),
		strings.Index(sql, "MIN("),
		strings.Index(sql, "MAX("),
	}
	for i, p := range positions {
		require.NotEqual(t, -1, p, "aggregate %d not found in SQL", i)
	}
	assert.IsIncreasing(t, positions)
}

func TestBuildGroupBySQL_WithWhere(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("active").Eq(true)},
	}
	sql, args, err := buildGroupBySQL("products", []string{"category"},
		[]den.GroupByAgg{{Op: den.OpCount}}, q)
	require.NoError(t, err)
	assert.Contains(t, sql, "WHERE")
	assert.Contains(t, sql, "GROUP BY")
	assert.Less(t, strings.Index(sql, "WHERE"), strings.Index(sql, "GROUP BY"))
	// Eq on a scalar is rewritten to a containment clause for the GIN index.
	assert.Contains(t, sql, "data @> $1::jsonb")
	assert.Equal(t, []any{[]byte(`{"active":true}`)}, args)
}

func TestBuildGroupBySQL_NestedField(t *testing.T) {
	q := &den.Query{Collection: "products"}
	sql, _, err := buildGroupBySQL("products", []string{"address.country"},
		[]den.GroupByAgg{{Op: den.OpCount}}, q)
	require.NoError(t, err)
	assert.Contains(t, sql, `jsonb_extract_path_text(data, 'address', 'country')`)
}

func TestBuildGroupBySQL_UnsupportedOp(t *testing.T) {
	q := &den.Query{Collection: "products"}
	_, _, err := buildGroupBySQL("products", []string{"category"},
		[]den.GroupByAgg{{Op: den.AggregateOp("INVALID"), Field: "price"}}, q)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported aggregate op in group-by")
}

func TestBuildGroupBySQL_EmptyAggs(t *testing.T) {
	// Empty aggs slice is valid: produces distinct group keys only.
	q := &den.Query{Collection: "products"}
	sql, _, err := buildGroupBySQL("products", []string{"category"}, nil, q)
	require.NoError(t, err)
	assert.Contains(t, sql, `SELECT jsonb_extract_path_text(data, 'category') FROM "products"`)
	assert.Contains(t, sql, `GROUP BY jsonb_extract_path_text(data, 'category')`)
	assert.NotContains(t, sql, "COUNT")
	assert.NotContains(t, sql, "SUM")
}
