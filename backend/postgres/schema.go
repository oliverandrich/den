package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oliverandrich/den"
)

func createTable(ctx context.Context, pool *pgxpool.Pool, name string) error {
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id TEXT PRIMARY KEY,
		data JSONB NOT NULL
	)`, quoteIdent(name))
	_, err := pool.Exec(ctx, query)
	if err != nil {
		return err
	}

	// Auto-create GIN index for containment queries
	ginQuery := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s USING GIN(data jsonb_path_ops)",
		quoteIdent("idx_"+name+"_gin"), quoteIdent(name))
	_, err = pool.Exec(ctx, ginQuery)
	return err
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
	if idx.Unique {
		uniqueStr = "UNIQUE "
	}

	whereClause := ""
	if idx.Unique {
		parts := make([]string, len(idx.Fields))
		for i, f := range idx.Fields {
			parts[i] = fmt.Sprintf("data->>'%s' IS NOT NULL", f)
		}
		whereClause = " WHERE " + strings.Join(parts, " AND ")
	}

	query := fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s(%s)%s",
		uniqueStr, quoteIdent(idx.Name), quoteIdent(collection), exprList, whereClause)
	_, err := pool.Exec(ctx, query)
	return err
}
