package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("name").Eq("Widget")},
	}
	sql, args := buildSelectSQL("products", q)
	assert.Contains(t, sql, "jsonb_extract_path(data, 'name') = $1::jsonb")
	assert.Equal(t, []any{`"Widget"`}, args)
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
	assert.Equal(t, []any{`"a"`, `"b"`}, args)
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
	assert.Equal(t, []any{`"Berlin"`}, args)
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
	assert.Equal(t, []any{`"A"`}, args)
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
	assert.Equal(t, []any{"10"}, args)
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
	assert.Equal(t, []any{`"Alpha"`}, args)
}

func TestBuildAggregateSQL_Sum(t *testing.T) {
	q := &den.Query{Collection: "products"}
	sql, args := buildAggregateSQL("products", den.OpSum, "price", q)
	assert.Contains(t, sql, `SUM((jsonb_extract_path_text(data, 'price'))::float)`)
	assert.Contains(t, sql, `FROM "products"`)
	assert.Empty(t, args)
}

func TestBuildAggregateSQL_AvgWithFilter(t *testing.T) {
	q := &den.Query{
		Collection: "products",
		Conditions: []where.Condition{where.Field("category").Eq("X")},
	}
	sql, args := buildAggregateSQL("products", den.OpAvg, "price", q)
	assert.Contains(t, sql, `AVG((jsonb_extract_path_text(data, 'price'))::float)`)
	assert.Contains(t, sql, "WHERE")
	assert.Equal(t, []any{`"X"`}, args)
}

func TestMapPGError_Nil(t *testing.T) {
	assert.NoError(t, mapPGError(nil))
}

func TestQuoteIdent(t *testing.T) {
	assert.Equal(t, `"products"`, quoteIdent("products"))
	assert.Equal(t, `"my""table"`, quoteIdent(`my"table`))
}
