package sqlite

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/where"
)

// sanitizeFieldName strips characters that are not safe for JSON path interpolation.
// Allows letters, digits, underscores, and dots (for nested paths).
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

// buildSelectSQL translates a den.Query into a SQLite SELECT statement.
func buildSelectSQL(collection string, q *den.Query) (string, []any) {
	var sb strings.Builder
	var args []any

	fmt.Fprintf(&sb, "SELECT id, json(data) FROM %q", collection)

	clauses, clauseArgs := buildWhereClauses(q.Conditions)
	hasClauses := len(clauses) > 0
	if hasClauses {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(clauses, " AND "))
		args = append(args, clauseArgs...)
	}

	// Cursor pagination
	if q.AfterID != "" {
		if hasClauses {
			sb.WriteString(" AND ")
		} else {
			sb.WriteString(" WHERE ")
		}
		sb.WriteString("id > ?")
		args = append(args, q.AfterID)
	}
	if q.BeforeID != "" {
		if hasClauses || q.AfterID != "" {
			sb.WriteString(" AND ")
		} else {
			sb.WriteString(" WHERE ")
		}
		sb.WriteString("id < ?")
		args = append(args, q.BeforeID)
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
			fmt.Fprintf(&sb, "json_extract(data, '$.%s') %s", sanitizeFieldName(s.Field), dir)
		}
	}

	if q.LimitN > 0 {
		fmt.Fprintf(&sb, " LIMIT %d", q.LimitN)
	}
	if q.SkipN > 0 {
		if q.LimitN == 0 {
			sb.WriteString(" LIMIT -1") // SQLite requires LIMIT before OFFSET
		}
		fmt.Fprintf(&sb, " OFFSET %d", q.SkipN)
	}

	return sb.String(), args
}

func appendCursorClauses(clauses []string, args []any, q *den.Query) ([]string, []any) {
	if q.AfterID != "" {
		clauses = append(clauses, "id > ?")
		args = append(args, q.AfterID)
	}
	if q.BeforeID != "" {
		clauses = append(clauses, "id < ?")
		args = append(args, q.BeforeID)
	}
	return clauses, args
}

func buildCountSQL(collection string, q *den.Query) (string, []any) {
	clauses, args := buildWhereClauses(q.Conditions)
	clauses, args = appendCursorClauses(clauses, args, q)
	sql := fmt.Sprintf("SELECT COUNT(*) FROM %q", collection)
	if len(clauses) > 0 {
		sql += " WHERE " + strings.Join(clauses, " AND ")
	}
	return sql, args
}

func buildExistsSQL(collection string, q *den.Query) (string, []any) {
	clauses, args := buildWhereClauses(q.Conditions)
	clauses, args = appendCursorClauses(clauses, args, q)
	inner := fmt.Sprintf("SELECT 1 FROM %q", collection)
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
	clauses, args := buildWhereClauses(q.Conditions)
	expr := fmt.Sprintf("CAST(json_extract(data, '$.%s') AS REAL)", sanitizeFieldName(field))
	sql := fmt.Sprintf("SELECT %s(%s) FROM %q", string(op), expr, collection)
	if len(clauses) > 0 {
		sql += " WHERE " + strings.Join(clauses, " AND ")
	}
	return sql, args
}

func buildWhereClauses(conditions []where.Condition) ([]string, []any) {
	var clauses []string
	var args []any

	for _, cond := range conditions {
		clause, clauseArgs := conditionToSQL(cond)
		if clause != "" {
			clauses = append(clauses, clause)
			args = append(args, clauseArgs...)
		}
	}

	return clauses, args
}

func conditionToSQL(cond where.Condition) (string, []any) {
	switch c := cond.(type) {
	case interface {
		FieldName() string
		Op() where.Operator
		Value() any
		Values() []any
	}:
		return fieldConditionToSQL(c.FieldName(), c.Op(), c.Value(), c.Values())
	case interface {
		Logic() where.LogicType
		Conditions() []where.Condition
	}:
		return logicalToSQL(c.Logic(), c.Conditions())
	case interface {
		Inner() where.Condition
	}:
		inner, args := conditionToSQL(c.Inner())
		if inner == "" {
			return "", nil
		}
		return fmt.Sprintf("NOT (%s)", inner), args
	default:
		return "", nil
	}
}

func fieldConditionToSQL(rawField string, op where.Operator, value any, values []any) (string, []any) {
	field := sanitizeFieldName(rawField)
	jsonPath := fmt.Sprintf("json_extract(data, '$.%s')", field)

	// FieldRef: compare against another document field instead of a parameter
	if ref, isRef := value.(where.FieldRef); isRef {
		refPath := fmt.Sprintf("json_extract(data, '$.%s')", sanitizeFieldName(string(ref)))
		return fieldRefToSQL(jsonPath, refPath, op), nil
	}

	switch op {
	case where.OpEq:
		return fmt.Sprintf("%s = ?", jsonPath), []any{value}
	case where.OpNe:
		return fmt.Sprintf("%s != ?", jsonPath), []any{value}
	case where.OpGt:
		return fmt.Sprintf("%s > ?", jsonPath), []any{value}
	case where.OpGte:
		return fmt.Sprintf("%s >= ?", jsonPath), []any{value}
	case where.OpLt:
		return fmt.Sprintf("%s < ?", jsonPath), []any{value}
	case where.OpLte:
		return fmt.Sprintf("%s <= ?", jsonPath), []any{value}
	case where.OpIsNil:
		return fmt.Sprintf("%s IS NULL", jsonPath), nil
	case where.OpIsNotNil:
		return fmt.Sprintf("%s IS NOT NULL", jsonPath), nil
	case where.OpIn:
		placeholders := make([]string, len(values))
		for i := range values {
			placeholders[i] = "?"
		}
		return fmt.Sprintf("%s IN (%s)", jsonPath, strings.Join(placeholders, ", ")), values
	case where.OpNotIn:
		placeholders := make([]string, len(values))
		for i := range values {
			placeholders[i] = "?"
		}
		return fmt.Sprintf("%s NOT IN (%s)", jsonPath, strings.Join(placeholders, ", ")), values
	case where.OpContains:
		return fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(json_extract(data, '$.%s')) WHERE value = ?)", field), []any{value}
	case where.OpContainsAny:
		placeholders := make([]string, len(values))
		for i := range values {
			placeholders[i] = "?"
		}
		return fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(json_extract(data, '$.%s')) WHERE value IN (%s))", field, strings.Join(placeholders, ", ")), values
	case where.OpContainsAll:
		clauses := make([]string, len(values))
		var allArgs []any
		for i, v := range values {
			clauses[i] = fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(json_extract(data, '$.%s')) WHERE value = ?)", field)
			allArgs = append(allArgs, v)
		}
		return strings.Join(clauses, " AND "), allArgs
	case where.OpHasKey:
		key, _ := value.(string)
		return fmt.Sprintf("json_type(data, '$.%s.%s') IS NOT NULL", field, sanitizeFieldName(key)), nil
	case where.OpRegExp:
		return fmt.Sprintf("%s REGEXP ?", jsonPath), []any{fmt.Sprintf("%v", value)}
	default:
		return "", nil
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

func logicalToSQL(logic where.LogicType, conditions []where.Condition) (string, []any) {
	var clauses []string
	var args []any

	for _, c := range conditions {
		clause, clauseArgs := conditionToSQL(c)
		if clause != "" {
			clauses = append(clauses, "("+clause+")")
			args = append(args, clauseArgs...)
		}
	}

	if len(clauses) == 0 {
		return "", nil
	}

	sep := " AND "
	if logic == where.LogicOr {
		sep = " OR "
	}
	return strings.Join(clauses, sep), args
}

// rowsIterator implements den.Iterator over sql.Rows.
type rowsIterator struct {
	data []byte
	id   string
	rows *sql.Rows
	err  error
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

func (it *rowsIterator) Close() error {
	return it.rows.Close()
}
