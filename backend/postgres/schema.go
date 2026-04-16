package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oliverandrich/den"
)

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
		exprs[i] = fmt.Sprintf("(data->>'%s')", f)
	}
	exprList := strings.Join(exprs, ", ")

	uniqueStr := ""
	whereClause := ""
	if idx.Unique {
		uniqueStr = "UNIQUE "
		parts := make([]string, len(idx.Fields))
		for i, f := range idx.Fields {
			parts[i] = fmt.Sprintf("data->>'%s' IS NOT NULL", f)
		}
		whereClause = " WHERE " + strings.Join(parts, " AND ")
	}

	query := fmt.Sprintf("CREATE %sINDEX CONCURRENTLY IF NOT EXISTS %s ON %s(%s)%s",
		uniqueStr, quoteIdent(idx.Name), quoteIdent(collection), exprList, whereClause)
	return ensureConcurrentIndex(ctx, pool, idx.Name, query)
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
