package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/goccy/go-json"

	"github.com/oliverandrich/den"
)

const metadataTableName = "_den_indexes"

func createTable(ctx context.Context, db *sql.DB, name string) error {
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %q (
		id TEXT PRIMARY KEY,
		data BLOB NOT NULL
	)`, name)
	_, err := db.ExecContext(ctx, query)
	return err
}

func ensureMetadataTable(ctx context.Context, db *sql.DB) error {
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %q (
		collection TEXT NOT NULL,
		name TEXT NOT NULL,
		fields TEXT NOT NULL,
		is_unique INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (collection, name)
	)`, metadataTableName)
	_, err := db.ExecContext(ctx, query)
	return err
}

func createExpressionIndex(ctx context.Context, db *sql.DB, collection string, idx den.IndexDefinition) error {
	if len(idx.Fields) == 0 {
		return nil
	}

	exprs := make([]string, len(idx.Fields))
	for i, f := range idx.Fields {
		exprs[i] = fmt.Sprintf("json_extract(data, '$.%s')", sanitizeFieldName(f))
	}
	exprList := strings.Join(exprs, ", ")

	uniqueStr := ""
	if idx.Unique {
		uniqueStr = "UNIQUE "
	}

	// For unique indexes, add a WHERE clause excluding NULLs
	whereClause := ""
	if idx.Unique {
		parts := make([]string, len(idx.Fields))
		for i, f := range idx.Fields {
			parts[i] = fmt.Sprintf("json_extract(data, '$.%s') IS NOT NULL", sanitizeFieldName(f))
		}
		whereClause = " WHERE " + strings.Join(parts, " AND ")
	}

	query := fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %q ON %q(%s)%s",
		uniqueStr, idx.Name, collection, exprList, whereClause)
	if _, err := db.ExecContext(ctx, query); err != nil {
		return err
	}
	return recordIndex(ctx, db, collection, idx)
}

func recordIndex(ctx context.Context, db *sql.DB, collection string, idx den.IndexDefinition) error {
	fieldsJSON, err := json.Marshal(idx.Fields)
	if err != nil {
		return err
	}
	unique := 0
	if idx.Unique {
		unique = 1
	}
	query := fmt.Sprintf(`INSERT INTO %q (collection, name, fields, is_unique, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (collection, name) DO UPDATE SET
			fields = excluded.fields,
			is_unique = excluded.is_unique`, metadataTableName)
	_, err = db.ExecContext(ctx, query, collection, idx.Name, string(fieldsJSON), unique, time.Now().UTC().Format(time.RFC3339))
	return err
}

func forgetIndex(ctx context.Context, db *sql.DB, name string) error {
	query := fmt.Sprintf(`DELETE FROM %q WHERE name = ?`, metadataTableName)
	_, err := db.ExecContext(ctx, query, name)
	return err
}

func listRecordedIndexes(ctx context.Context, db *sql.DB, collection string) ([]den.RecordedIndex, error) {
	query := fmt.Sprintf(`SELECT name, fields, is_unique FROM %q WHERE collection = ? ORDER BY name`, metadataTableName)
	rows, err := db.QueryContext(ctx, query, collection)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []den.RecordedIndex
	for rows.Next() {
		var name, fieldsJSON string
		var unique int
		if err := rows.Scan(&name, &fieldsJSON, &unique); err != nil {
			return nil, err
		}
		var fields []string
		if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
			return nil, err
		}
		result = append(result, den.RecordedIndex{
			Name:   name,
			Fields: fields,
			Unique: unique != 0,
		})
	}
	return result, rows.Err()
}
