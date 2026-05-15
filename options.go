package den

import (
	"github.com/oliverandrich/den/internal/core"
)

// WithLinkRule sets the link cascading rule for Save / Delete and the
// QuerySet write terminals.
func WithLinkRule(rule LinkRule) CRUDOption {
	return core.WithLinkRule(rule)
}

// WithoutFetchLinks suppresses link hydration on a doc-in-hand read,
// including fields tagged `den:"eager"`. Honored by FindByID, FindByIDs,
// and Refresh.
func WithoutFetchLinks() CRUDOption {
	return core.WithoutFetchLinks()
}

// HardDelete permanently removes a soft-deleteable document instead of
// flipping its DeletedAt.
func HardDelete() CRUDOption {
	return core.HardDelete()
}

// IncludeDeleted makes by-ID lookups (FindByID, FindByIDs) consider
// soft-deleted documents. Mirrors QuerySet.IncludeDeleted().
func IncludeDeleted() CRUDOption {
	return core.IncludeDeleted()
}

// SoftDeleteBy records an actor on the document's DeletedBy field on the
// soft-delete path. No-op on HardDelete() and on types that don't embed
// `document.SoftDelete`.
func SoftDeleteBy(actor string) CRUDOption {
	return core.SoftDeleteBy(actor)
}

// SoftDeleteReason records a free-form reason on the document's
// DeleteReason field on the soft-delete path.
func SoftDeleteReason(reason string) CRUDOption {
	return core.SoftDeleteReason(reason)
}

// IgnoreRevision skips the optimistic-concurrency revision check on
// Save's update path. Use sparingly — race losers get silently
// overwritten.
func IgnoreRevision() CRUDOption {
	return core.IgnoreRevision()
}

// NoWait makes LockByID return ErrLocked immediately when the row is
// already locked, instead of blocking. PostgreSQL only.
func NoWait() LockOption {
	return core.NoWait()
}

// SkipLocked makes LockByID return ErrNotFound when the row is already
// locked, instead of blocking. PostgreSQL only.
func SkipLocked() LockOption {
	return core.SkipLocked()
}
