package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/oliverandrich/den"
)

// EnsureFTS creates a tsvector generated column and GIN index for FTS.
func (b *backend) EnsureFTS(ctx context.Context, collection string, fields []string) error {
	// Build tsvector expression from FTS fields
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = fmt.Sprintf("coalesce(data->>'%s', '')", sanitizeFieldName(f))
	}
	tsvectorExpr := fmt.Sprintf("to_tsvector('english', %s)", strings.Join(parts, " || ' ' || "))

	// Add generated column (ignore error if already exists)
	alterSQL := fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN IF NOT EXISTS _fts_vector tsvector GENERATED ALWAYS AS (%s) STORED",
		quoteIdent(collection), tsvectorExpr,
	)
	if _, err := b.pool.Exec(ctx, alterSQL); err != nil {
		return fmt.Errorf("add tsvector column: %w", err)
	}

	// Create GIN index on the tsvector column
	indexSQL := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s USING GIN(_fts_vector)",
		quoteIdent("idx_"+collection+"_fts"), quoteIdent(collection),
	)
	if _, err := b.pool.Exec(ctx, indexSQL); err != nil {
		return fmt.Errorf("create FTS GIN index: %w", err)
	}

	return nil
}

// Search performs a full-text search using tsquery.
func (b *backend) Search(ctx context.Context, collection string, query string, q *den.Query) (den.Iterator, error) {
	var sb strings.Builder
	var args []any
	paramN := 1

	fmt.Fprintf(&sb,
		"SELECT id, data::text FROM %s WHERE _fts_vector @@ plainto_tsquery('english', $%d)",
		quoteIdent(collection), paramN,
	)
	args = append(args, query)
	paramN++

	// Add where conditions
	if len(q.Conditions) > 0 {
		for _, cond := range q.Conditions {
			clause, clauseArgs, nextN := conditionToSQL(cond, paramN)
			if clause != "" {
				sb.WriteString(" AND ")
				sb.WriteString(clause)
				args = append(args, clauseArgs...)
				paramN = nextN
			}
		}
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
			fmt.Fprintf(&sb, "data->>'%s' %s", sanitizeFieldName(s.Field), dir)
		}
	} else {
		sb.WriteString(" ORDER BY ts_rank(_fts_vector, plainto_tsquery('english', $1)) DESC")
	}

	if q.LimitN > 0 {
		fmt.Fprintf(&sb, " LIMIT %d", q.LimitN)
	}
	if q.SkipN > 0 {
		fmt.Fprintf(&sb, " OFFSET %d", q.SkipN)
	}

	rows, err := b.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	return &rowsIterator{rows: rows}, nil
}
