package where

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestField_Eq(t *testing.T) {
	c := Field("price").Eq(10)
	require.IsType(t, &fieldCondition{}, c)
	fc := c.(*fieldCondition)
	assert.Equal(t, "price", fc.field)
	assert.Equal(t, OpEq, fc.op)
	assert.Equal(t, 10, fc.value)
}

func TestField_Comparison(t *testing.T) {
	tests := []struct {
		cond Condition
		name string
		op   Operator
	}{
		{name: "Ne", cond: Field("x").Ne(1), op: OpNe},
		{name: "Gt", cond: Field("x").Gt(1), op: OpGt},
		{name: "Gte", cond: Field("x").Gte(1), op: OpGte},
		{name: "Lt", cond: Field("x").Lt(1), op: OpLt},
		{name: "Lte", cond: Field("x").Lte(1), op: OpLte},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := tt.cond.(*fieldCondition)
			assert.Equal(t, tt.op, fc.op)
		})
	}
}

func TestField_In_NotIn(t *testing.T) {
	c := Field("status").In("active", "pending")
	fc := c.(*fieldCondition)
	assert.Equal(t, OpIn, fc.op)
	assert.Equal(t, []any{"active", "pending"}, fc.values)

	c = Field("status").NotIn("deleted")
	fc = c.(*fieldCondition)
	assert.Equal(t, OpNotIn, fc.op)
}

// TestAnyOf_StringSlice pins the typed-slice spread shortcut: AnyOf
// converts []string → []any, then In/NotIn spread it into the same
// underlying condition shape as a literal variadic call would produce.
func TestAnyOf_StringSlice(t *testing.T) {
	ids := []string{"a", "b", "c"}
	c := Field("id").In(AnyOf(ids)...)
	fc := c.(*fieldCondition)
	assert.Equal(t, OpIn, fc.op)
	assert.Equal(t, []any{"a", "b", "c"}, fc.values,
		"AnyOf must convert each element into []any so In sees them as N values, not one slice")
}

// TestAnyOf_Int64Slice pins that the helper is generic over the
// element type. A typed []int64 spreads identically; In does not
// silently accept the slice as a single value.
func TestAnyOf_Int64Slice(t *testing.T) {
	versions := []int64{1, 2, 3}
	c := Field("version").NotIn(AnyOf(versions)...)
	fc := c.(*fieldCondition)
	assert.Equal(t, OpNotIn, fc.op)
	assert.Equal(t, []any{int64(1), int64(2), int64(3)}, fc.values)
}

// TestAnyOf_Empty pins the empty-slice contract: an empty input
// produces zero values, which the backends interpret as "match
// nothing" (In) or "match everything" (NotIn).
func TestAnyOf_Empty(t *testing.T) {
	var ids []string
	out := AnyOf(ids)
	assert.Empty(t, out, "empty input → empty []any output")
}

func TestField_IsNil_IsNotNil(t *testing.T) {
	c := Field("read_at").IsNil()
	fc := c.(*fieldCondition)
	assert.Equal(t, OpIsNil, fc.op)

	c = Field("read_at").IsNotNil()
	fc = c.(*fieldCondition)
	assert.Equal(t, OpIsNotNil, fc.op)
}

func TestField_Contains(t *testing.T) {
	c := Field("tags").Contains("golang")
	fc := c.(*fieldCondition)
	assert.Equal(t, OpContains, fc.op)
	assert.Equal(t, "golang", fc.value)
}

func TestField_ContainsAny(t *testing.T) {
	c := Field("tags").ContainsAny("go", "golang")
	fc := c.(*fieldCondition)
	assert.Equal(t, OpContainsAny, fc.op)
	assert.Equal(t, []any{"go", "golang"}, fc.values)
}

func TestField_ContainsAll(t *testing.T) {
	c := Field("tags").ContainsAll("go", "golang")
	fc := c.(*fieldCondition)
	assert.Equal(t, OpContainsAll, fc.op)
	assert.Equal(t, []any{"go", "golang"}, fc.values)
}

func TestField_DotNotation(t *testing.T) {
	c := Field("address.city").Eq("Berlin")
	fc := c.(*fieldCondition)
	assert.Equal(t, "address.city", fc.field)
}

func TestAnd(t *testing.T) {
	c := And(Field("x").Gt(1), Field("x").Lt(10))
	lc := c.(*logicalCondition)
	assert.Equal(t, LogicAnd, lc.logic)
	assert.Len(t, lc.conditions, 2)
}

func TestOr(t *testing.T) {
	c := Or(Field("a").Eq(1), Field("b").Eq(2))
	lc := c.(*logicalCondition)
	assert.Equal(t, LogicOr, lc.logic)
	assert.Len(t, lc.conditions, 2)
}

func TestNot(t *testing.T) {
	c := Not(Field("deleted").Eq(true))
	nc := c.(*notCondition)
	assert.NotNil(t, nc.inner)
}

func TestField_HasKey(t *testing.T) {
	c := Field("metadata").HasKey("color")
	fc := c.(*fieldCondition)
	assert.Equal(t, OpHasKey, fc.op)
	assert.Equal(t, "color", fc.value)
}

func TestField_RegExp(t *testing.T) {
	c := Field("name").RegExp("pattern")
	fc := c.(*fieldCondition)
	assert.Equal(t, OpRegExp, fc.op)
	assert.Equal(t, "pattern", fc.value)
}

func TestFieldRef(t *testing.T) {
	// Field-vs-field: end > start
	c := Field("end").Gt(FieldRef("start"))
	fc := c.(*fieldCondition)
	assert.Equal(t, OpGt, fc.op)
	_, isRef := fc.value.(FieldRef)
	assert.True(t, isRef)
}

func TestCondition_FieldName(t *testing.T) {
	c := Field("price").Gt(10)
	assert.Equal(t, "price", c.FieldName())
}

func TestCondition_FieldName_Logical(t *testing.T) {
	c := And(Field("x").Gt(1), Field("y").Lt(10))
	assert.Empty(t, c.FieldName())
}

func TestCondition_FieldAccessors(t *testing.T) {
	fc := Field("price").Gt(42).(*fieldCondition)
	assert.Equal(t, OpGt, fc.Op())
	assert.Equal(t, 42, fc.Value())
	assert.Nil(t, fc.Values())

	fc2 := Field("status").In("a", "b").(*fieldCondition)
	assert.Equal(t, OpIn, fc2.Op())
	assert.Equal(t, []any{"a", "b"}, fc2.Values())
}

func TestCondition_LogicalAccessors(t *testing.T) {
	lc := And(Field("x").Gt(1), Field("y").Lt(10)).(*logicalCondition)
	assert.Equal(t, LogicAnd, lc.Logic())
	assert.Len(t, lc.Conditions(), 2)

	lc2 := Or(Field("a").Eq(1)).(*logicalCondition)
	assert.Equal(t, LogicOr, lc2.Logic())
	assert.Len(t, lc2.Conditions(), 1)
}

func TestCondition_NotAccessors(t *testing.T) {
	nc := Not(Field("deleted").Eq(true)).(*notCondition)
	assert.NotNil(t, nc.Inner())
	assert.Empty(t, nc.FieldName())
}
