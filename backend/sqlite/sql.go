package sqlite

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/internal"
	"github.com/oliverandrich/den/where"
)

var sanitizeFieldName = internal.SanitizeFieldName

// buildSelectSQL translates a den.Query into a SQLite SELECT statement.
func buildSelectSQL(collection string, q *den.Query) (string, []any) {
	var sb strings.Builder

	fmt.Fprintf(&sb, "SELECT id, json(data) FROM %q", collection)

	clauses, args := buildWhereClauses(q.Conditions)
	clauses, args = appendCursorClauses(clauses, args, q)
	if len(clauses) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(clauses, " AND "))
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

func buildAggregateSQL(collection string, op den.AggregateOp, field string, q *den.Query) (string, []any, error) {
	switch op { //nolint:exhaustive // OpCount is only valid in GroupBy, not scalar Aggregate
	case den.OpSum, den.OpAvg, den.OpMin, den.OpMax:
	default:
		return "", nil, fmt.Errorf("den: unsupported aggregate op: %s", op)
	}
	clauses, args := buildWhereClauses(q.Conditions)
	expr := fmt.Sprintf("CAST(json_extract(data, '$.%s') AS REAL)", sanitizeFieldName(field))
	sql := fmt.Sprintf("SELECT %s(%s) FROM %q", string(op), expr, collection)
	if len(clauses) > 0 {
		sql += " WHERE " + strings.Join(clauses, " AND ")
	}
	return sql, args, nil
}

func buildGroupBySQL(collection string, groupFields []string, aggs []den.GroupByAgg, q *den.Query) (string, []any, error) {
	if len(groupFields) == 0 {
		return "", nil, fmt.Errorf("den: GroupBy requires at least one field")
	}

	groupExprs := make([]string, len(groupFields))
	for i, f := range groupFields {
		groupExprs[i] = fmt.Sprintf("json_extract(data, '$.%s')", sanitizeFieldName(f))
	}

	// Build SELECT: group keys + aggregate expressions
	selectParts := append([]string(nil), groupExprs...)
	for _, agg := range aggs {
		switch agg.Op {
		case den.OpCount:
			selectParts = append(selectParts, "COUNT(*)")
		case den.OpSum, den.OpAvg, den.OpMin, den.OpMax:
			af := sanitizeFieldName(agg.Field)
			expr := fmt.Sprintf("CAST(json_extract(data, '$.%s') AS REAL)", af)
			selectParts = append(selectParts, fmt.Sprintf("%s(%s)", string(agg.Op), expr))
		default:
			return "", nil, fmt.Errorf("den: unsupported aggregate op in group-by: %s", agg.Op)
		}
	}

	clauses, args := buildWhereClauses(q.Conditions)
	clauses, args = appendCursorClauses(clauses, args, q)

	var sb strings.Builder
	fmt.Fprintf(&sb, "SELECT %s FROM %q", strings.Join(selectParts, ", "), collection)
	if len(clauses) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(clauses, " AND "))
	}
	fmt.Fprintf(&sb, " GROUP BY %s", strings.Join(groupExprs, ", "))

	// ORDER BY: group-key sorts first (from SortFields), then aggregate
	// sorts (from GroupBySort). Aggregate reconstruction matches the
	// expressions emitted in SELECT above.
	var orderParts []string
	for _, sf := range q.SortFields {
		// Into() already validated sf.Field is a group key.
		for i, gf := range groupFields {
			if gf == sf.Field {
				orderParts = append(orderParts, groupExprs[i]+sortDirSQL(sf.Dir))
				break
			}
		}
	}
	for _, gs := range q.GroupBySort {
		expr, err := groupAggExprSQLite(gs)
		if err != nil {
			return "", nil, err
		}
		orderParts = append(orderParts, expr+sortDirSQL(gs.Dir))
	}
	if len(orderParts) > 0 {
		fmt.Fprintf(&sb, " ORDER BY %s", strings.Join(orderParts, ", "))
	}

	if q.LimitN > 0 {
		fmt.Fprintf(&sb, " LIMIT %d", q.LimitN)
	}
	if q.SkipN > 0 {
		if q.LimitN == 0 {
			sb.WriteString(" LIMIT -1")
		}
		fmt.Fprintf(&sb, " OFFSET %d", q.SkipN)
	}

	return sb.String(), args, nil
}

func sortDirSQL(dir den.SortDirection) string {
	if dir == den.Desc {
		return " DESC"
	}
	return " ASC"
}

func groupAggExprSQLite(gs den.GroupBySortEntry) (string, error) {
	switch gs.Op {
	case den.OpCount:
		return "COUNT(*)", nil
	case den.OpSum, den.OpAvg, den.OpMin, den.OpMax:
		return fmt.Sprintf("%s(CAST(json_extract(data, '$.%s') AS REAL))",
			string(gs.Op), sanitizeFieldName(gs.Field)), nil
	default:
		return "", fmt.Errorf("den: unsupported aggregate op in order-by: %s", gs.Op)
	}
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
	case where.OpStartsWith:
		return fmt.Sprintf("%s LIKE ? ESCAPE '\\'", jsonPath), []any{escapeLike(fmt.Sprintf("%v", value)) + "%"}
	case where.OpEndsWith:
		return fmt.Sprintf("%s LIKE ? ESCAPE '\\'", jsonPath), []any{"%" + escapeLike(fmt.Sprintf("%v", value))}
	case where.OpStringContains:
		return fmt.Sprintf("%s LIKE ? ESCAPE '\\'", jsonPath), []any{"%" + escapeLike(fmt.Sprintf("%v", value)) + "%"}
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

var escapeLike = internal.EscapeLike

func (it *rowsIterator) Bytes() []byte { return it.data }
func (it *rowsIterator) ID() string    { return it.id }
func (it *rowsIterator) Err() error    { return it.err }

func (it *rowsIterator) Close() error {
	return it.rows.Close()
}
