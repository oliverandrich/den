package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oliverandrich/den"
)

const metadataTableName = "_den_indexes"

func ensureMetadataTable(ctx context.Context, pool *pgxpool.Pool) error {
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		collection TEXT NOT NULL,
		name TEXT NOT NULL,
		fields JSONB NOT NULL,
		is_unique BOOLEAN NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (collection, name)
	)`, quoteIdent(metadataTableName))
	_, err := pool.Exec(ctx, query)
	return err
}

func recordIndex(ctx context.Context, pool *pgxpool.Pool, collection string, idx den.IndexDefinition) error {
	fieldsJSON, err := json.Marshal(idx.Fields)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`INSERT INTO %s (collection, name, fields, is_unique, created_at)
		VALUES ($1, $2, $3::jsonb, $4, $5)
		ON CONFLICT (collection, name) DO UPDATE SET
			fields = EXCLUDED.fields,
			is_unique = EXCLUDED.is_unique`, quoteIdent(metadataTableName))
	_, err = pool.Exec(ctx, query, collection, idx.Name, string(fieldsJSON), idx.Unique, time.Now().UTC())
	return err
}

func forgetIndex(ctx context.Context, pool *pgxpool.Pool, name string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE name = $1`, quoteIdent(metadataTableName))
	_, err := pool.Exec(ctx, query, name)
	return err
}

func listRecordedIndexes(ctx context.Context, pool *pgxpool.Pool, collection string) ([]den.RecordedIndex, error) {
	query := fmt.Sprintf(`SELECT name, fields, is_unique FROM %s WHERE collection = $1 ORDER BY name`, quoteIdent(metadataTableName))
	rows, err := pool.Query(ctx, query, collection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []den.RecordedIndex
	for rows.Next() {
		var name string
		var fieldsJSON []byte
		var unique bool
		if err := rows.Scan(&name, &fieldsJSON, &unique); err != nil {
			return nil, err
		}
		var fields []string
		if err := json.Unmarshal(fieldsJSON, &fields); err != nil {
			return nil, err
		}
		result = append(result, den.RecordedIndex{
			Name:   name,
			Fields: fields,
			Unique: unique,
		})
	}
	return result, rows.Err()
}

func createTable(ctx context.Context, pool *pgxpool.Pool, name string) error {
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id TEXT PRIMARY KEY,
		data JSONB NOT NULL
	)`, quoteIdent(name))
	if _, err := pool.Exec(ctx, query); err != nil {
		return err
	}

	ginIndexName := "idx_" + name + "_gin"
	ginQuery := fmt.Sprintf("CREATE INDEX CONCURRENTLY IF NOT EXISTS %s ON %s USING GIN(data jsonb_path_ops)",
		quoteIdent(ginIndexName), quoteIdent(name))
	return ensureConcurrentIndex(ctx, pool, ginIndexName, ginQuery)
}

func createExpressionIndex(ctx context.Context, pool *pgxpool.Pool, collection string, idx den.IndexDefinition) error {
	if len(idx.Fields) == 0 {
		return nil
	}

	exprs := make([]string, len(idx.Fields))
	for i, f := range idx.Fields {
		exprs[i] = fmt.Sprintf("(data->>'%s')", sanitizeFieldName(f))
	}
	exprList := strings.Join(exprs, ", ")

	uniqueStr := ""
	whereClause := ""
	if idx.Unique {
		uniqueStr = "UNIQUE "
		parts := make([]string, len(idx.Fields))
		for i, f := range idx.Fields {
			parts[i] = fmt.Sprintf("data->>'%s' IS NOT NULL", sanitizeFieldName(f))
		}
		whereClause = " WHERE " + strings.Join(parts, " AND ")
	}

	query := fmt.Sprintf("CREATE %sINDEX CONCURRENTLY IF NOT EXISTS %s ON %s(%s)%s",
		uniqueStr, quoteIdent(idx.Name), quoteIdent(collection), exprList, whereClause)
	if err := ensureConcurrentIndex(ctx, pool, idx.Name, query); err != nil {
		return err
	}
	return recordIndex(ctx, pool, collection, idx)
}

// ensureConcurrentIndex drops any existing invalid index with the same name
// (left behind by an aborted CREATE INDEX CONCURRENTLY) and then runs the
// provided create statement, which must use CREATE INDEX CONCURRENTLY IF NOT EXISTS.
func ensureConcurrentIndex(ctx context.Context, pool *pgxpool.Pool, indexName, createSQL string) error {
	invalid, err := isIndexInvalid(ctx, pool, indexName)
	if err != nil {
		return err
	}
	if invalid {
		dropSQL := fmt.Sprintf("DROP INDEX CONCURRENTLY IF EXISTS %s", quoteIdent(indexName))
		if _, err := pool.Exec(ctx, dropSQL); err != nil {
			return err
		}
	}
	_, err = pool.Exec(ctx, createSQL)
	return err
}

// isIndexInvalid reports whether an index with the given name exists in the
// current schema and is marked invalid.
func isIndexInvalid(ctx context.Context, pool *pgxpool.Pool, indexName string) (bool, error) {
	var valid bool
	err := pool.QueryRow(ctx, `
		SELECT i.indisvalid
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relname = $1 AND n.nspname = current_schema()
	`, indexName).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !valid, nil
}
