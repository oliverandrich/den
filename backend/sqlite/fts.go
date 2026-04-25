package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/oliverandrich/den"
)

// EnsureFTS creates an FTS5 virtual table and sync triggers for the collection.
func (b *backend) EnsureFTS(ctx context.Context, collection string, fields []string) error {
	ftsTable := collection + "_fts"

	// Defense in depth: sanitize every field name before it lands in the bare
	// FTS column list. Registration-time validation should already have rejected
	// anything unsafe, but sanitize again here so no raw %s of a field name
	// survives SQL construction.
	sanitized := make([]string, len(fields))
	for i, f := range fields {
		sanitized[i] = sanitizeFieldName(f)
	}
	fieldList := strings.Join(sanitized, ", ")

	// Create FTS5 virtual table
	createFTS := fmt.Sprintf(
		"CREATE VIRTUAL TABLE IF NOT EXISTS %q USING fts5(%s, content=%q, content_rowid=rowid)",
		ftsTable, fieldList, collection,
	)
	if _, err := b.db.ExecContext(ctx, createFTS); err != nil {
		return fmt.Errorf("create FTS5 table: %w", err)
	}

	// Triggers to keep FTS in sync
	// INSERT trigger
	insertExprs := make([]string, len(fields))
	for i, f := range fields {
		insertExprs[i] = fmt.Sprintf("json_extract(NEW.data, '$.%s')", sanitizeFieldName(f))
	}
	insertTrigger := fmt.Sprintf( //nolint:gosec // table/column names from internal registration
		`CREATE TRIGGER IF NOT EXISTS %q AFTER INSERT ON %q BEGIN
			INSERT INTO %q(rowid, %s) VALUES (NEW.rowid, %s);
		END`,
		collection+"_fts_insert", collection, ftsTable, fieldList, strings.Join(insertExprs, ", "),
	)
	if _, err := b.db.ExecContext(ctx, insertTrigger); err != nil {
		return fmt.Errorf("create FTS insert trigger: %w", err)
	}

	// DELETE trigger
	deleteExprs := make([]string, len(fields))
	for i, f := range fields {
		deleteExprs[i] = fmt.Sprintf("json_extract(OLD.data, '$.%s')", sanitizeFieldName(f))
	}
	deleteTrigger := fmt.Sprintf( //nolint:gosec // table/column names from internal registration
		`CREATE TRIGGER IF NOT EXISTS %q BEFORE DELETE ON %q BEGIN
			INSERT INTO %q(%q, rowid, %s) VALUES ('delete', OLD.rowid, %s);
		END`,
		collection+"_fts_delete", collection, ftsTable, ftsTable, fieldList, strings.Join(deleteExprs, ", "),
	)
	if _, err := b.db.ExecContext(ctx, deleteTrigger); err != nil {
		return fmt.Errorf("create FTS delete trigger: %w", err)
	}

	// UPDATE trigger
	updateTrigger := fmt.Sprintf( //nolint:gosec // table/column names from internal registration
		`CREATE TRIGGER IF NOT EXISTS %q AFTER UPDATE ON %q BEGIN
			INSERT INTO %q(%q, rowid, %s) VALUES ('delete', OLD.rowid, %s);
			INSERT INTO %q(rowid, %s) VALUES (NEW.rowid, %s);
		END`,
		collection+"_fts_update", collection,
		ftsTable, ftsTable, fieldList, strings.Join(deleteExprs, ", "),
		ftsTable, fieldList, strings.Join(insertExprs, ", "),
	)
	if _, err := b.db.ExecContext(ctx, updateTrigger); err != nil {
		return fmt.Errorf("create FTS update trigger: %w", err)
	}

	return nil
}

// buildFTSSearchSQL constructs the FTS5 MATCH query for the collection.
// Shared by the *DB and *Tx Search implementations so the SQL stays in
// one place; only the executor differs.
func buildFTSSearchSQL(collection, query string, q *den.Query) (string, []any) {
	ftsTable := collection + "_fts"

	var sb strings.Builder
	args := []any{query}

	fmt.Fprintf(&sb,
		"SELECT t.id, json(t.data) FROM %q t JOIN %q f ON t.rowid = f.rowid WHERE %q MATCH ?",
		collection, ftsTable, ftsTable,
	)

	if len(q.Conditions) > 0 {
		for _, cond := range q.Conditions {
			clause, clauseArgs := conditionToSQL(cond)
			if clause != "" {
				// Prefix table references for the joined query
				sb.WriteString(" AND ")
				sb.WriteString(strings.ReplaceAll(clause, "json_extract(data,", "json_extract(t.data,"))
				args = append(args, clauseArgs...)
			}
		}
	}

	if q.AfterID != "" {
		sb.WriteString(" AND t.id > ?")
		args = append(args, q.AfterID)
	}
	if q.BeforeID != "" {
		sb.WriteString(" AND t.id < ?")
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
			fmt.Fprintf(&sb, "json_extract(t.data, '$.%s') %s", sanitizeFieldName(s.Field), dir)
		}
	} else {
		sb.WriteString(" ORDER BY rank")
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

	return sb.String(), args
}

// Search performs a full-text search using FTS5 MATCH against the *DB
// connection. Reads committed state; for tx-local visibility see the
// transaction's Search method.
func (b *backend) Search(ctx context.Context, collection string, query string, q *den.Query) (den.Iterator, error) {
	sqlStr, args := buildFTSSearchSQL(collection, query, q)
	rows, err := b.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	return &rowsIterator{rows: rows}, nil
}
