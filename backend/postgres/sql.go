package postgres

import (
	"fmt"
	"strings"

	json "github.com/goccy/go-json"
	"github.com/jackc/pgx/v5"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/where"
)

// toJSONBParam serializes a Go value to its JSON representation for use as a
// $N::jsonb parameter. This preserves type information: strings become JSON
// strings, numbers become JSON numbers, etc.
func toJSONBParam(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func sanitizeFieldName(field string) string {
	var b strings.Builder
	for _, r := range field {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// jsonbPath returns a typed JSONB extraction expression that preserves the
// original JSON type (number, string, boolean). Supports nested fields via
// dot notation: "address.city" → jsonb_extract_path(data, 'address', 'city').
func jsonbPath(rawField string) string {
	field := sanitizeFieldName(rawField)
	parts := strings.Split(field, ".")
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = "'" + p + "'"
	}
	return "jsonb_extract_path(data, " + strings.Join(quoted, ", ") + ")"
}

// jsonbPathText returns a text extraction expression. Used for text operations
// (LIKE, REGEXP) and IS NULL checks where SQL NULL semantics are needed.
func jsonbPathText(rawField string) string {
	field := sanitizeFieldName(rawField)
	parts := strings.Split(field, ".")
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = "'" + p + "'"
	}
	return "jsonb_extract_path_text(data, " + strings.Join(quoted, ", ") + ")"
}

// buildSelectSQL translates a den.Query into a PostgreSQL SELECT statement.
func buildSelectSQL(collection string, q *den.Query) (string, []any) {
	var sb strings.Builder

	fmt.Fprintf(&sb, "SELECT id, data::text FROM %s", quoteIdent(collection))

	whereClauses, args, paramN := buildWhereClauses(q.Conditions)

	if q.AfterID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("id > $%d", paramN))
		args = append(args, q.AfterID)
		paramN++
	}
	if q.BeforeID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("id < $%d", paramN))
		args = append(args, q.BeforeID)
	}

	if len(whereClauses) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(whereClauses, " AND "))
	}

	if len(q.SortFields) > 0 {
		sb.WriteString(" ORDER BY ")
		for i, s := range q.SortFields {
			if i > 0 {
				sb.WriteString(", ")
			}
			dir := "ASC"
			if s.Dir == den.Desc {
				dir = "DESC"
			}
			fmt.Fprintf(&sb, "%s %s", jsonbPath(s.Field), dir)
		}
	}

	if q.LimitN > 0 {
		fmt.Fprintf(&sb, " LIMIT %d", q.LimitN)
	}
	if q.SkipN > 0 {
		fmt.Fprintf(&sb, " OFFSET %d", q.SkipN)
	}

	return sb.String(), args
}

func buildWhereClauses(conditions []where.Condition) ([]string, []any, int) {
	var clauses []string
	var args []any
	paramN := 1
	for _, cond := range conditions {
		clause, clauseArgs, nextN := conditionToSQL(cond, paramN)
		if clause != "" {
			clauses = append(clauses, clause)
			args = append(args, clauseArgs...)
			paramN = nextN
		}
	}
	return clauses, args, paramN
}

func appendCursorClauses(clauses []string, args []any, paramN int, q *den.Query) ([]string, []any) {
	if q.AfterID != "" {
		clauses = append(clauses, fmt.Sprintf("id > $%d", paramN))
		args = append(args, q.AfterID)
		paramN++
	}
	if q.BeforeID != "" {
		clauses = append(clauses, fmt.Sprintf("id < $%d", paramN))
		args = append(args, q.BeforeID)
	}
	return clauses, args
}

func buildCountSQL(collection string, q *den.Query) (string, []any) {
	clauses, args, paramN := buildWhereClauses(q.Conditions)
	clauses, args = appendCursorClauses(clauses, args, paramN, q)
	sql := fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdent(collection))
	if len(clauses) > 0 {
		sql += " WHERE " + strings.Join(clauses, " AND ")
	}
	return sql, args
}

func buildExistsSQL(collection string, q *den.Query) (string, []any) {
	clauses, args, paramN := buildWhereClauses(q.Conditions)
	clauses, args = appendCursorClauses(clauses, args, paramN, q)
	inner := fmt.Sprintf("SELECT 1 FROM %s", quoteIdent(collection))
	if len(clauses) > 0 {
		inner += " WHERE " + strings.Join(clauses, " AND ")
	}
	inner += " LIMIT 1"
	return fmt.Sprintf("SELECT EXISTS(%s)", inner), args
}

func buildAggregateSQL(collection string, op den.AggregateOp, field string, q *den.Query) (string, []any) {
	switch op {
	case den.OpSum, den.OpAvg, den.OpMin, den.OpMax:
	default:
		panic(fmt.Sprintf("den: unsupported aggregate op: %s", op))
	}
	clauses, args, _ := buildWhereClauses(q.Conditions)
	expr := fmt.Sprintf("(%s)::float", jsonbPathText(field))
	sql := fmt.Sprintf("SELECT %s(%s) FROM %s", string(op), expr, quoteIdent(collection))
	if len(clauses) > 0 {
		sql += " WHERE " + strings.Join(clauses, " AND ")
	}
	return sql, args
}

func conditionToSQL(cond where.Condition, paramN int) (string, []any, int) {
	switch c := cond.(type) {
	case interface {
		FieldName() string
		Op() where.Operator
		Value() any
		Values() []any
	}:
		return fieldConditionToSQL(c.FieldName(), c.Op(), c.Value(), c.Values(), paramN)
	case interface {
		Logic() where.LogicType
		Conditions() []where.Condition
	}:
		return logicalToSQL(c.Logic(), c.Conditions(), paramN)
	case interface {
		Inner() where.Condition
	}:
		inner, args, nextN := conditionToSQL(c.Inner(), paramN)
		if inner == "" {
			return "", nil, paramN
		}
		return fmt.Sprintf("NOT (%s)", inner), args, nextN
	default:
		return "", nil, paramN
	}
}

func fieldConditionToSQL(rawField string, op where.Operator, value any, values []any, paramN int) (string, []any, int) {
	typed := jsonbPath(rawField)
	text := jsonbPathText(rawField)

	// FieldRef: compare against another document field
	if ref, isRef := value.(where.FieldRef); isRef {
		refPath := jsonbPath(string(ref))
		return fieldRefToSQL(typed, refPath, op), nil, paramN
	}

	switch op {
	case where.OpEq:
		return fmt.Sprintf("%s = $%d::jsonb", typed, paramN), []any{toJSONBParam(value)}, paramN + 1
	case where.OpNe:
		return fmt.Sprintf("%s != $%d::jsonb", typed, paramN), []any{toJSONBParam(value)}, paramN + 1
	case where.OpGt:
		return fmt.Sprintf("%s > $%d::jsonb", typed, paramN), []any{toJSONBParam(value)}, paramN + 1
	case where.OpGte:
		return fmt.Sprintf("%s >= $%d::jsonb", typed, paramN), []any{toJSONBParam(value)}, paramN + 1
	case where.OpLt:
		return fmt.Sprintf("%s < $%d::jsonb", typed, paramN), []any{toJSONBParam(value)}, paramN + 1
	case where.OpLte:
		return fmt.Sprintf("%s <= $%d::jsonb", typed, paramN), []any{toJSONBParam(value)}, paramN + 1
	case where.OpIsNil:
		return fmt.Sprintf("%s IS NULL", text), nil, paramN
	case where.OpIsNotNil:
		return fmt.Sprintf("%s IS NOT NULL", text), nil, paramN
	case where.OpIn:
		placeholders := make([]string, len(values))
		jsonbValues := make([]any, len(values))
		for i := range values {
			placeholders[i] = fmt.Sprintf("$%d::jsonb", paramN+i)
			jsonbValues[i] = toJSONBParam(values[i])
		}
		return fmt.Sprintf("%s IN (%s)", typed, strings.Join(placeholders, ", ")), jsonbValues, paramN + len(values)
	case where.OpNotIn:
		placeholders := make([]string, len(values))
		jsonbValues := make([]any, len(values))
		for i := range values {
			placeholders[i] = fmt.Sprintf("$%d::jsonb", paramN+i)
			jsonbValues[i] = toJSONBParam(values[i])
		}
		return fmt.Sprintf("%s NOT IN (%s)", typed, strings.Join(placeholders, ", ")), jsonbValues, paramN + len(values)
	case where.OpContains:
		return fmt.Sprintf("%s @> to_jsonb($%d::text)", typed, paramN), []any{value}, paramN + 1
	case where.OpContainsAny:
		clauses := make([]string, len(values))
		var args []any
		for i, v := range values {
			clauses[i] = fmt.Sprintf("%s @> to_jsonb($%d::text)", typed, paramN+i)
			args = append(args, v)
		}
		return "(" + strings.Join(clauses, " OR ") + ")", args, paramN + len(values)
	case where.OpContainsAll:
		clauses := make([]string, len(values))
		var args []any
		for i, v := range values {
			clauses[i] = fmt.Sprintf("%s @> to_jsonb($%d::text)", typed, paramN+i)
			args = append(args, v)
		}
		return strings.Join(clauses, " AND "), args, paramN + len(values)
	case where.OpHasKey:
		return fmt.Sprintf("jsonb_exists(%s, $%d)", typed, paramN), []any{value}, paramN + 1
	case where.OpRegExp:
		return fmt.Sprintf("%s ~ $%d", text, paramN), []any{fmt.Sprintf("%v", value)}, paramN + 1
	case where.OpStartsWith:
		return fmt.Sprintf("%s LIKE $%d ESCAPE '\\'", text, paramN), []any{escapeLike(fmt.Sprintf("%v", value)) + "%"}, paramN + 1
	case where.OpEndsWith:
		return fmt.Sprintf("%s LIKE $%d ESCAPE '\\'", text, paramN), []any{"%" + escapeLike(fmt.Sprintf("%v", value))}, paramN + 1
	case where.OpStringContains:
		return fmt.Sprintf("%s LIKE $%d ESCAPE '\\'", text, paramN), []any{"%" + escapeLike(fmt.Sprintf("%v", value)) + "%"}, paramN + 1
	default:
		return "", nil, paramN
	}
}

func fieldRefToSQL(left, right string, op where.Operator) string {
	switch op { //nolint:exhaustive // only comparison operators apply to field refs

	case where.OpEq:
		return fmt.Sprintf("%s = %s", left, right)
	case where.OpNe:
		return fmt.Sprintf("%s != %s", left, right)
	case where.OpGt:
		return fmt.Sprintf("%s > %s", left, right)
	case where.OpGte:
		return fmt.Sprintf("%s >= %s", left, right)
	case where.OpLt:
		return fmt.Sprintf("%s < %s", left, right)
	case where.OpLte:
		return fmt.Sprintf("%s <= %s", left, right)
	default:
		return ""
	}
}

func logicalToSQL(logic where.LogicType, conditions []where.Condition, paramN int) (string, []any, int) {
	var clauses []string
	var args []any

	for _, c := range conditions {
		clause, clauseArgs, nextN := conditionToSQL(c, paramN)
		if clause != "" {
			clauses = append(clauses, "("+clause+")")
			args = append(args, clauseArgs...)
			paramN = nextN
		}
	}

	if len(clauses) == 0 {
		return "", nil, paramN
	}

	sep := " AND "
	if logic == where.LogicOr {
		sep = " OR "
	}
	return strings.Join(clauses, sep), args, paramN
}

// rowsIterator implements den.Iterator over pgx.Rows.
type rowsIterator struct {
	data []byte
	id   string
	rows pgx.Rows
	err  error
}

// escapeLike escapes LIKE special characters (%, _, \) in a value.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func (it *rowsIterator) Next() bool {
	if !it.rows.Next() {
		it.err = it.rows.Err()
		return false
	}
	if err := it.rows.Scan(&it.id, &it.data); err != nil {
		it.err = err
		return false
	}
	return true
}

func (it *rowsIterator) Bytes() []byte { return it.data }
func (it *rowsIterator) ID() string    { return it.id }
func (it *rowsIterator) Err() error    { return it.err }
func (it *rowsIterator) Close() error  { it.rows.Close(); return nil }
