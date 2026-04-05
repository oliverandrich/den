package where

// Operator represents a comparison operator.
type Operator int

const (
	OpEq Operator = iota
	OpNe
	OpGt
	OpGte
	OpLt
	OpLte
	OpIn
	OpNotIn
	OpIsNil
	OpIsNotNil
	OpContains
	OpContainsAny
	OpContainsAll
	OpRegExp
	OpHasKey
	OpStartsWith
	OpEndsWith
	OpStringContains
)

// FieldRef references another field for field-vs-field comparisons.
// Example: where.Field("end").Gt(where.FieldRef("start"))
type FieldRef string

// LogicType represents a logical combinator.
type LogicType int

const (
	LogicAnd LogicType = iota
	LogicOr
)

// Condition represents a query filter condition.
type Condition interface {
	// FieldName returns the target field name, or "" for logical conditions.
	FieldName() string
	// condition is a marker method to prevent external implementations.
	condition()
}

// FieldBuilder provides a fluent API for building field conditions.
type FieldBuilder struct {
	name string
}

// Field starts building a condition on the named field.
// Supports dot notation for nested fields (e.g. "address.city").
func Field(name string) FieldBuilder {
	return FieldBuilder{name: name}
}

func (fb FieldBuilder) Eq(value any) Condition {
	return &fieldCondition{field: fb.name, op: OpEq, value: value}
}

func (fb FieldBuilder) Ne(value any) Condition {
	return &fieldCondition{field: fb.name, op: OpNe, value: value}
}

func (fb FieldBuilder) Gt(value any) Condition {
	return &fieldCondition{field: fb.name, op: OpGt, value: value}
}

func (fb FieldBuilder) Gte(value any) Condition {
	return &fieldCondition{field: fb.name, op: OpGte, value: value}
}

func (fb FieldBuilder) Lt(value any) Condition {
	return &fieldCondition{field: fb.name, op: OpLt, value: value}
}

func (fb FieldBuilder) Lte(value any) Condition {
	return &fieldCondition{field: fb.name, op: OpLte, value: value}
}

func (fb FieldBuilder) In(values ...any) Condition {
	return &fieldCondition{field: fb.name, op: OpIn, values: values}
}

func (fb FieldBuilder) NotIn(values ...any) Condition {
	return &fieldCondition{field: fb.name, op: OpNotIn, values: values}
}

func (fb FieldBuilder) IsNil() Condition {
	return &fieldCondition{field: fb.name, op: OpIsNil}
}

func (fb FieldBuilder) IsNotNil() Condition {
	return &fieldCondition{field: fb.name, op: OpIsNotNil}
}

func (fb FieldBuilder) Contains(value any) Condition {
	return &fieldCondition{field: fb.name, op: OpContains, value: value}
}

func (fb FieldBuilder) ContainsAny(values ...any) Condition {
	return &fieldCondition{field: fb.name, op: OpContainsAny, values: values}
}

func (fb FieldBuilder) ContainsAll(values ...any) Condition {
	return &fieldCondition{field: fb.name, op: OpContainsAll, values: values}
}

// StartsWith matches string fields that start with the given prefix.
func (fb FieldBuilder) StartsWith(prefix string) Condition {
	return &fieldCondition{field: fb.name, op: OpStartsWith, value: prefix}
}

// EndsWith matches string fields that end with the given suffix.
func (fb FieldBuilder) EndsWith(suffix string) Condition {
	return &fieldCondition{field: fb.name, op: OpEndsWith, value: suffix}
}

// StringContains matches string fields that contain the given substring.
func (fb FieldBuilder) StringContains(substr string) Condition {
	return &fieldCondition{field: fb.name, op: OpStringContains, value: substr}
}

// HasKey checks whether a map field contains the given key.
func (fb FieldBuilder) HasKey(key string) Condition {
	return &fieldCondition{field: fb.name, op: OpHasKey, value: key}
}

// RegExp matches the field value against a compiled regular expression.
func (fb FieldBuilder) RegExp(re any) Condition {
	return &fieldCondition{field: fb.name, op: OpRegExp, value: re}
}

// And combines conditions with logical AND.
func And(conditions ...Condition) Condition {
	return &logicalCondition{logic: LogicAnd, conditions: conditions}
}

// Or combines conditions with logical OR.
func Or(conditions ...Condition) Condition {
	return &logicalCondition{logic: LogicOr, conditions: conditions}
}

// Not negates a condition.
func Not(c Condition) Condition {
	return &notCondition{inner: c}
}

// fieldCondition is a condition on a single field.
type fieldCondition struct {
	value  any
	field  string
	values []any
	op     Operator
}

func (c *fieldCondition) FieldName() string { return c.field }
func (c *fieldCondition) condition()        {}

// Op returns the operator for this condition.
func (c *fieldCondition) Op() Operator { return c.op }

// Value returns the single comparison value.
func (c *fieldCondition) Value() any { return c.value }

// Values returns the multi-value operand (for In, NotIn, ContainsAny, ContainsAll).
func (c *fieldCondition) Values() []any { return c.values }

// logicalCondition combines multiple conditions with AND or OR.
type logicalCondition struct {
	conditions []Condition
	logic      LogicType
}

func (c *logicalCondition) FieldName() string { return "" }
func (c *logicalCondition) condition()        {}

// Logic returns the logical operator type.
func (c *logicalCondition) Logic() LogicType { return c.logic }

// Conditions returns the child conditions.
func (c *logicalCondition) Conditions() []Condition { return c.conditions }

// notCondition negates a condition.
type notCondition struct {
	inner Condition
}

func (c *notCondition) FieldName() string { return "" }
func (c *notCondition) condition()        {}

// Inner returns the negated condition.
func (c *notCondition) Inner() Condition { return c.inner }
