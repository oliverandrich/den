// SPDX-License-Identifier: MIT

// Package search defines the full-text-search contract that backends (and,
// where supported, their transactions) implement. Backends opt into FTS by
// satisfying [FTSProvider] (which extends [FTSSearcher] with the
// registration-time setup hook); transactions implement only
// [FTSSearcher] because index/trigger creation is a one-time setup
// operation that does not belong on a transactional path.
//
// Application code reaches FTS through QuerySet.Search at the den root —
// direct imports of this package are only needed when building a custom
// backend.
package search

import (
	"context"
	"strings"

	"github.com/oliverandrich/den/backend"
)

// LiteralFTS5 turns a raw user term into a literal-terms FTS5 MATCH
// expression: each whitespace-separated token becomes a double-quoted
// literal (embedded quotes doubled), tokens joined by spaces so FTS5 ANDs
// them. This neutralises FTS5 operators and punctuation (AND/OR/NEAR,
// col:term scoping, prefix *, stray quotes) that would otherwise raise a
// syntax error or let a client scope columns. An all-blank term yields "".
//
// The same string is safe to feed to PostgreSQL plainto_tsquery, which
// discards the quotes as punctuation and ANDs the surviving lexemes — so
// QuerySet.Search applies this once and dispatches the result to either
// backend unchanged.
func LiteralFTS5(term string) string {
	var b strings.Builder
	for tok := range strings.FieldsSeq(term) {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte('"')
		b.WriteString(strings.ReplaceAll(tok, `"`, `""`))
		b.WriteByte('"')
	}
	return b.String()
}

// FTSSearcher is the read-side full-text search contract. Both backends and
// transactions implement it so QuerySet.Search honors the caller's scope:
// `NewQuery[T](db).Search(...)` reads committed state, while
// `NewQuery[T](tx).Search(...)` sees the tx's uncommitted writes (the FTS
// index is updated in-tx by triggers on SQLite and by tsvector + GIN under
// MVCC on PostgreSQL).
type FTSSearcher interface {
	Search(ctx context.Context, collection string, query string, q *backend.Query) (backend.Iterator, error)
}

// FTSProvider extends [FTSSearcher] with the registration-time setup hook.
// Backends implement the full interface; transactions implement only
// [FTSSearcher] because index/trigger creation is a one-time setup
// operation that does not belong on a transactional path.
//
// EnsureFTS must be idempotent, and after it returns, rows that existed in
// the collection before the first call must be searchable — first-time
// setup backfills the index from existing data rather than indexing only
// subsequent writes.
type FTSProvider interface {
	FTSSearcher
	EnsureFTS(ctx context.Context, collection string, fields []string) error
}
