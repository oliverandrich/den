package engine

import (
	"context"

	"github.com/oliverandrich/den/where"
)

// FindByID retrieves a document by its ID. Returns ErrNotFound if no
// row matches.
//
// `den:"eager"`-tagged link fields on T are hydrated by default; pass
// WithoutFetchLinks to suppress hydration. Soft-deleted documents are
// returned: explicit-by-ID lookups bypass the soft-delete filter that
// QuerySet read terminals apply on filtered queries — callers can check
// Value.IsDeleted() to react.
//
// Top-level shorthand for `NewQuery[T](s).Where(where.Field("_id").Eq(id)).IncludeDeleted().First(ctx)`
// — discoverable next to Save / Delete / Refresh.
func FindByID[T any](ctx context.Context, s Scope, id string, opts ...CRUDOption) (*T, error) {
	return querySetFromOpts[T](s, []where.Condition{where.Field("_id").Eq(id)}, opts).IncludeDeleted().First(ctx)
}

// FindByIDs retrieves multiple documents by their IDs in a single query.
// Missing IDs are silently skipped. Order is not guaranteed.
//
// `den:"eager"`-tagged link fields on T are batch-resolved by default;
// pass WithoutFetchLinks to suppress hydration. Soft-deleted documents
// are returned (see FindByID for the rationale).
//
// Top-level shorthand for `NewQuery[T](s).Where(where.Field("_id").In(ids...)).IncludeDeleted().All(ctx)`.
func FindByIDs[T any](ctx context.Context, s Scope, ids []string, opts ...CRUDOption) ([]*T, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	anyIDs := make([]any, len(ids))
	for i, id := range ids {
		anyIDs[i] = id
	}
	return querySetFromOpts[T](s, []where.Condition{where.Field("_id").In(anyIDs...)}, opts).IncludeDeleted().All(ctx)
}

// querySetFromOpts translates the legacy CRUDOption surface (includeDeleted,
// fetchMode) into the equivalent QuerySet state. Internal helper for FindByID
// / FindByIDs, which still expose the option-based API.
func querySetFromOpts[T any](s Scope, conditions []where.Condition, opts []CRUDOption) QuerySet[T] {
	o := applyCRUDOpts(opts)
	qs := NewQuery[T](s, conditions...)
	if o.includeDeleted {
		qs = qs.IncludeDeleted()
	}
	switch crudFetchMode(o) { //nolint:exhaustive // fetchDefault matches the QuerySet's NewQuery default — no override needed.
	case fetchAll:
		qs = qs.WithFetchLinks()
	case fetchNone:
		qs = qs.WithoutFetchLinks()
	}
	return qs
}

// findOneStrict loads exactly one document matching conditions. Returns
// ErrNotFound if none match, ErrMultipleMatches if more than one matches.
//
// Limit(2) lets the backend stop after the second row — enough to detect
// non-uniqueness without scanning the full match set.
func findOneStrict[T any](
	ctx context.Context,
	s Scope,
	conditions []where.Condition,
	includeDeleted bool,
) (*T, error) {
	qs := NewQuery[T](s, conditions...).Limit(2)
	if includeDeleted {
		qs = qs.IncludeDeleted()
	}
	results, err := qs.All(ctx)
	if err != nil {
		return nil, err
	}
	switch len(results) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return results[0], nil
	default:
		return nil, ErrMultipleMatches
	}
}
