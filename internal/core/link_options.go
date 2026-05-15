package core

// LinkRule controls cascading behavior for write and delete operations.
//
// LinkDelete cascades a Delete to the immediate link targets only — it does
// not recurse into the targets' own links. Callers that need transitive
// cleanup must walk the graph themselves. This keeps a mis-configured delete
// from wiping an unbounded subgraph.
type LinkRule int

const (
	LinkIgnore LinkRule = iota
	LinkWrite
	LinkDelete
)

// CRUDOption configures CRUD operations.
type CRUDOption func(*crudOpts)

type crudOpts struct {
	linkRule           LinkRule
	ignoreRevision     bool
	hardDelete         bool
	includeDeleted     bool
	suppressFetchLinks bool
	softDeleteBy       string
	softDeleteReason   string
}

// WithLinkRule sets the link cascading rule for Save / Delete and the
// QuerySet write terminals.
func WithLinkRule(rule LinkRule) CRUDOption {
	return func(o *crudOpts) {
		o.linkRule = rule
	}
}

// WithoutFetchLinks suppresses link hydration on a doc-in-hand read,
// including fields tagged `den:"eager"`. Mirrors the QuerySet modifier
// of the same name; honored by FindByID, FindByIDs, and Refresh. On a
// type with no eager-tagged links it's a no-op.
func WithoutFetchLinks() CRUDOption {
	return func(o *crudOpts) {
		o.suppressFetchLinks = true
	}
}

// crudFetchMode returns the fetch mode a CRUD-style read should use:
// fetchNone when the caller passed WithoutFetchLinks, fetchDefault
// otherwise (which honors the eager tag on the result type).
func crudFetchMode(o crudOpts) fetchMode {
	if o.suppressFetchLinks {
		return fetchNone
	}
	return fetchDefault
}

func applyCRUDOpts(opts []CRUDOption) crudOpts {
	var o crudOpts
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
