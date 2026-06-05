package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/internal/util"
)

// execer abstracts *sql.DB and *sql.Tx so the FTS trigger setup can run
// either bare or inside the first-creation transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// EnsureFTS creates an FTS5 virtual table and sync triggers for the
// collection. On first creation it also backfills the index from rows
// that already exist — without this, documents saved before the fts tag
// was added would stay invisible to Search until their next write.
func (b *backend) EnsureFTS(ctx context.Context, collection string, fields []string) error {
	ftsTable := collection + "_fts"

	// Two derived forms per field. The JSON path keeps dots so
	// `json_extract(data, '$.profile.bio')` resolves nested fields.
	// The FTS5 column name has dots stripped because
	// `CREATE VIRTUAL TABLE … fts5(profile.bio, …)` would parse the
	// dot as a table.column qualifier. sanitizeFieldName here is
	// defence-in-depth — registration-time validation already filtered
	// unsafe runes.
	jsonPaths := make([]string, len(fields))
	columnNames := make([]string, len(fields))
	for i, f := range fields {
		jsonPaths[i] = sanitizeFieldName(f)
		columnNames[i] = util.IdentifierSegment(jsonPaths[i])
	}
	fieldList := strings.Join(columnNames, ", ")

	var exists bool
	err := b.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)",
		ftsTable,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check FTS5 table existence: %w", err)
	}

	if exists {
		// Idempotent re-registration: the index is already in sync via
		// the triggers; just make sure they are present.
		return createFTSTriggers(ctx, b.db, collection, fieldList, jsonPaths)
	}

	// First creation: table, triggers and backfill commit atomically. The
	// table's existence is the only skip-backfill signal future Register
	// calls have, so a partial failure must not leave the table behind.
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin FTS setup: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// No IF NOT EXISTS: absence was just checked, and a concurrent
	// Register losing the race must error and roll back rather than
	// backfill twice.
	createFTS := fmt.Sprintf(
		"CREATE VIRTUAL TABLE %q USING fts5(%s, content=%q, content_rowid=rowid)",
		ftsTable, fieldList, collection,
	)
	if _, err := tx.ExecContext(ctx, createFTS); err != nil {
		return fmt.Errorf("create FTS5 table: %w", err)
	}

	if err := createFTSTriggers(ctx, tx, collection, fieldList, jsonPaths); err != nil {
		return err
	}

	// Backfill pre-existing rows using the same extraction expressions as
	// the insert trigger, against the stored column instead of NEW.data.
	backfill := fmt.Sprintf( //nolint:gosec // table/column names from internal registration
		"INSERT INTO %q(rowid, %s) SELECT rowid, %s FROM %q",
		ftsTable, fieldList, ftsExtractExprs("data", jsonPaths), collection,
	)
	if _, err := tx.ExecContext(ctx, backfill); err != nil {
		return fmt.Errorf("backfill FTS index: %w", err)
	}

	return tx.Commit()
}

// ftsExtractExprs renders the comma-joined json_extract expressions for the
// given source column (data, NEW.data, OLD.data). Triggers and backfill share
// it so their extraction expressions cannot drift apart.
func ftsExtractExprs(src string, jsonPaths []string) string {
	exprs := make([]string, len(jsonPaths))
	for i, p := range jsonPaths {
		exprs[i] = fmt.Sprintf("json_extract(%s, '$.%s')", src, p)
	}
	return strings.Join(exprs, ", ")
}

// createFTSTriggers installs the insert/delete/update sync triggers that
// keep the FTS index aligned with the collection table.
func createFTSTriggers(ctx context.Context, db execer, collection, fieldList string, jsonPaths []string) error {
	ftsTable := collection + "_fts"
	insertExprs := ftsExtractExprs("NEW.data", jsonPaths)
	deleteExprs := ftsExtractExprs("OLD.data", jsonPaths)

	// INSERT trigger
	insertTrigger := fmt.Sprintf( //nolint:gosec // table/column names from internal registration
		`CREATE TRIGGER IF NOT EXISTS %q AFTER INSERT ON %q BEGIN
			INSERT INTO %q(rowid, %s) VALUES (NEW.rowid, %s);
		END`,
		collection+"_fts_insert", collection, ftsTable, fieldList, insertExprs,
	)
	if _, err := db.ExecContext(ctx, insertTrigger); err != nil {
		return fmt.Errorf("create FTS insert trigger: %w", err)
	}

	// DELETE trigger
	deleteTrigger := fmt.Sprintf( //nolint:gosec // table/column names from internal registration
		`CREATE TRIGGER IF NOT EXISTS %q BEFORE DELETE ON %q BEGIN
			INSERT INTO %q(%q, rowid, %s) VALUES ('delete', OLD.rowid, %s);
		END`,
		collection+"_fts_delete", collection, ftsTable, ftsTable, fieldList, deleteExprs,
	)
	if _, err := db.ExecContext(ctx, deleteTrigger); err != nil {
		return fmt.Errorf("create FTS delete trigger: %w", err)
	}

	// UPDATE trigger
	updateTrigger := fmt.Sprintf( //nolint:gosec // table/column names from internal registration
		`CREATE TRIGGER IF NOT EXISTS %q AFTER UPDATE ON %q BEGIN
			INSERT INTO %q(%q, rowid, %s) VALUES ('delete', OLD.rowid, %s);
			INSERT INTO %q(rowid, %s) VALUES (NEW.rowid, %s);
		END`,
		collection+"_fts_update", collection,
		ftsTable, ftsTable, fieldList, deleteExprs,
		ftsTable, fieldList, insertExprs,
	)
	if _, err := db.ExecContext(ctx, updateTrigger); err != nil {
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
				// Prefix table references for the joined query. Wrap each
				// clause: AND > OR precedence would otherwise let an
				// Or(a,b) sibling absorb the MATCH predicate.
				sb.WriteString(" AND (")
				sb.WriteString(strings.ReplaceAll(clause, "json_extract(data,", "json_extract(t.data,"))
				sb.WriteString(")")
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
