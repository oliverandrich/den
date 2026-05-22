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

	"github.com/oliverandrich/den/backend"
)

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
type FTSProvider interface {
	FTSSearcher
	EnsureFTS(ctx context.Context, collection string, fields []string) error
}
