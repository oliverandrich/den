package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/where"
)

func TestBuildSelectSQL_NoConditions(t *testing.T) {
	q := &den.Query{Collection: "products"}
	sql, args := buildSelectSQL("products", q)
	assert.Equal(t, `SELECT id, json(data) FROM "products"`, sql)
	assert.Empty(t, args)
}

func TestBuildSelectSQL_WithEq(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").Eq("Widget")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "json_extract(data, '$.name') = ?")
	assert.Equal(t, []any{"Widget"}, args)
}

func TestBuildSelectSQL_Sort(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		SortFields: []den.SortEntry{{Field: "price", Dir: den.Asc}},
	}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "ORDER BY json_extract(data, '$.price') ASC")
}

func TestBuildSelectSQL_SortDesc(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		SortFields: []den.SortEntry{{Field: "price", Dir: den.Desc}},
	}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "DESC")
}

func TestBuildSelectSQL_LimitSkip(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		LimitN:     10,
		SkipN:      5,
	}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "LIMIT 10")
	assert.Contains(t, sql, "OFFSET 5")
}

func TestBuildSelectSQL_SkipWithoutLimit(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		SkipN:      5,
	}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "LIMIT -1")
	assert.Contains(t, sql, "OFFSET 5")
}

func TestBuildSelectSQL_Cursor(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		AfterID:    "p5",
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "id > ?")
	assert.Contains(t, args, "p5")
}

func TestBuildSelectSQL_BeforeCursor(t *testing.T) {
	q := &den.Query{Collection: "products", BeforeID: "p3"}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "id < ?")
	assert.Contains(t, args, "p3")
}

func TestBuildSelectSQL_BothCursors(t *testing.T) {
	q := &den.Query{Collection: "products", AfterID: "p1", BeforeID: "p5"}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "id > ?")
	assert.Contains(t, sql, "id < ?")
	assert.Equal(t, []any{"p1", "p5"}, args)
}

func TestBuildSelectSQL_IsNil(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("deleted_at").IsNil()},
	}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "IS NULL")
}

func TestBuildSelectSQL_In(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("status").In("active", "pending")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "IN (?, ?)")
	assert.Equal(t, []any{"active", "pending"}, args)
}

func TestBuildSelectSQL_And(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{
			where.And(
				where.Field("price").Gt(10),
				where.Field("price").Lt(100),
			),
		},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "AND")
	assert.Len(t, args, 2)
}

func TestBuildSelectSQL_Or(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{
			where.Or(
				where.Field("status").Eq("active"),
				where.Field("status").Eq("pending"),
			),
		},
	}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "OR")
}

func TestBuildSelectSQL_Not(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Not(where.Field("deleted").Eq(true))},
	}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "NOT")
}

func TestBuildSelectSQL_Contains(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("tags").Contains("golang")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "json_each")
	assert.Equal(t, []any{"golang"}, args)
}

func TestBuildSelectSQL_Comparison(t *testing.T) {
	ops := []struct {
		cond where.Condition
		want string
	}{
		{where.Field("x").Ne(1), "!="},
		{where.Field("x").Gte(1), ">="},
		{where.Field("x").Lte(1), "<="},
		{where.Field("x").IsNotNil(), "IS NOT NULL"},
		{where.Field("x").NotIn("a"), "NOT IN"},
	}
	for _, tt := range ops {
		q := &den.Query{Collection: "t", Conditions: []where.Condition{tt.cond}}
		sql, _ := buildSelectSQL("t", q)
		assert.Contains(t, sql, tt.want)
	}
}

func TestBuildSelectSQL_ContainsAny(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("tags").ContainsAny("a", "b")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "json_each")
	assert.Contains(t, sql, "IN (?, ?)")
	assert.Equal(t, []any{"a", "b"}, args)
}

func TestBuildSelectSQL_RegExp(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").RegExp("^Widget")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "REGEXP ?")
	assert.Equal(t, []any{"^Widget"}, args)
}

func TestBuildSelectSQL_FieldRef(t *testing.T) {
	q := &den.Query{
		Collection: "events",
		Conditions: []where.Condition{where.Field("end").Gt(where.FieldRef("start"))},
	}
	sql, args := buildSelectSQL("events", q)
	assert.Contains(t, sql, "json_extract(data, '$.end') > json_extract(data, '$.start')")
	assert.Empty(t, args) // no parameters, both sides are expressions
}

func TestBuildSelectSQL_HasKey(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("metadata").HasKey("color")},
	}
	sql, _ := buildSelectSQL("products", q)
	assert.Contains(t, sql, "json_type(data, '$.metadata.color') IS NOT NULL")
}

func TestBuildSelectSQL_FieldRef_AllOps(t *testing.T) {
	ops := []struct {
		op   where.Operator
		cond where.Condition
		want string
	}{
		{where.OpEq, where.Field("end").Eq(where.FieldRef("start")), "="},
		{where.OpNe, where.Field("end").Ne(where.FieldRef("start")), "!="},
		{where.OpGte, where.Field("end").Gte(where.FieldRef("start")), ">="},
		{where.OpLt, where.Field("end").Lt(where.FieldRef("start")), "<"},
		{where.OpLte, where.Field("end").Lte(where.FieldRef("start")), "<="},
	}
	for _, tt := range ops {
		q := &den.Query{Collection: "events", Conditions: []where.Condition{tt.cond}}
		sql, args := buildSelectSQL("events", q)
		assert.Contains(t, sql, "json_extract(data, '$.end') "+tt.want+" json_extract(data, '$.start')")
		assert.Empty(t, args)
	}
}

func TestBuildSelectSQL_ContainsAll(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("tags").ContainsAll("a", "b")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "json_each")
	assert.Len(t, args, 2)
}

func TestBuildCountSQL(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("price").Gt(10)},
	}
	sql, args := buildCountSQL("products", q)
	assert.Equal(t, `SELECT COUNT(*) FROM "products" WHERE json_extract(data, '$.price') > ?`, sql)
	assert.Equal(t, []any{10}, args)
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
	assert.Equal(t, []any{"Alpha"}, args)
}

func TestBuildAggregateSQL_Sum(t *testing.T) {
	q := &den.Query{Collection: "products"}
	sql, args, err := buildAggregateSQL("products", den.OpSum, "price", q)
	require.NoError(t, err)
	assert.Contains(t, sql, `SUM(CAST(json_extract(data, '$.price') AS REAL))`)
	assert.Contains(t, sql, `FROM "products"`)
	assert.Empty(t, args)
}

func TestSanitizeFieldName(t *testing.T) {
	assert.Equal(t, "price", sanitizeFieldName("price"))
	assert.Equal(t, "address.city", sanitizeFieldName("address.city"))
	assert.Equal(t, "nameDROPTABLEusers", sanitizeFieldName("name'; DROP TABLE users; --"))
	assert.Equal(t, "xOR11", sanitizeFieldName("x') OR '1'='1"))
	assert.Empty(t, sanitizeFieldName("'; --"))
}

func TestBuildAggregateSQL_AvgWithFilter(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("category").Eq("X")},
	}
	sql, args, err := buildAggregateSQL("products", den.OpAvg, "price", q)
	require.NoError(t, err)
	assert.Contains(t, sql, `AVG(CAST(json_extract(data, '$.price') AS REAL))`)
	assert.Contains(t, sql, "WHERE")
	assert.Equal(t, []any{"X"}, args)
}

func TestBuildAggregateSQL_UnsupportedOp(t *testing.T) {
	q := &den.Query{Collection: "products"}
	_, _, err := buildAggregateSQL("products", den.AggregateOp("INVALID"), "price", q)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported aggregate op")
}
