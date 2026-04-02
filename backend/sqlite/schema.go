package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/oliverandrich/den"
)

func createTable(ctx context.Context, db *sql.DB, name string) error {
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %q (
		id TEXT PRIMARY KEY,
		data BLOB NOT NULL
	)`, name)
	_, err := db.ExecContext(ctx, query)
	return err
}

func createExpressionIndex(ctx context.Context, db *sql.DB, collection string, idx den.IndexDefinition) error {
	if len(idx.Fields) == 0 {
		return nil
	}

	exprs := make([]string, len(idx.Fields))
	for i, f := range idx.Fields {
		exprs[i] = fmt.Sprintf("json_extract(data, '$.%s')", f)
	}
	exprList := strings.Join(exprs, ", ")

	uniqueStr := ""
	if idx.Unique {
		uniqueStr = "UNIQUE "
	}

	// For nullable unique indexes, add a WHERE clause
	whereClause := ""
	if idx.Unique && len(idx.Fields) == 1 {
		whereClause = fmt.Sprintf(" WHERE json_extract(data, '$.%s') IS NOT NULL", idx.Fields[0])
	}

	query := fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %q ON %q(%s)%s",
		uniqueStr, idx.Name, collection, exprList, whereClause)
	_, err := db.ExecContext(ctx, query)
	return err
}
