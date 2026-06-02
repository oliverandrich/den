package den

import (
	"context"

	"github.com/oliverandrich/den/engine"
)

// Save inserts the document if its ID is empty, otherwise updates it.
// The single doc-in-hand persistence entry point.
func Save[T any](ctx context.Context, s Scope, document *T, opts ...CRUDOption) error {
	return engine.Save(ctx, s, document, opts...)
}

// SaveAll persists every doc in docs in a single transaction. Fail-fast:
// any per-doc error rolls back the batch.
func SaveAll[T any](ctx context.Context, s Scope, docs []*T, opts ...CRUDOption) error {
	return engine.SaveAll(ctx, s, docs, opts...)
}

// Delete removes a document. Soft-deletes when the document embeds
// `document.SoftDelete`; pass HardDelete() to bypass.
func Delete[T any](ctx context.Context, s Scope, document *T, opts ...CRUDOption) error {
	return engine.Delete(ctx, s, document, opts...)
}

// DeleteAll deletes every doc in docs in a single transaction. Fail-fast.
func DeleteAll[T any](ctx context.Context, s Scope, docs []*T, opts ...CRUDOption) error {
	return engine.DeleteAll(ctx, s, docs, opts...)
}

// FindByID retrieves a document by its ID. Returns ErrNotFound if no row
// matches. Explicit-by-ID lookups bypass the soft-delete filter — callers
// can check Value.IsDeleted() to react.
func FindByID[T any](ctx context.Context, s Scope, id string, opts ...CRUDOption) (*T, error) {
	return engine.FindByID[T](ctx, s, id, opts...)
}

// FindByIDs retrieves multiple documents by their IDs in a single query.
// Missing IDs are silently skipped.
func FindByIDs[T any](ctx context.Context, s Scope, ids []string, opts ...CRUDOption) ([]*T, error) {
	return engine.FindByIDs[T](ctx, s, ids, opts...)
}

// Refresh re-reads a document from the database by its ID, overwriting
// all fields on the provided struct.
func Refresh[T any](ctx context.Context, s Scope, document *T, opts ...CRUDOption) error {
	return engine.Refresh(ctx, s, document, opts...)
}

// RefreshAll re-reads every doc in docs in a single transaction.
func RefreshAll[T any](ctx context.Context, s Scope, docs []*T, opts ...CRUDOption) error {
	return engine.RefreshAll(ctx, s, docs, opts...)
}

// Replace performs a full-content replace (PUT semantics): fresh's
// client-owned fields overwrite the stored row — omitted fields reset to
// zero — while Den's server-owned identity, audit, and soft-delete fields
// are preserved from the existing record. Last-writer-wins (adopts the
// stored revision); does not resurrect soft-deleted documents. Returns
// ErrNotFound if no row matches fresh's ID, ErrValidation if it has none.
func Replace[T any](ctx context.Context, s Scope, fresh *T, opts ...CRUDOption) error {
	return engine.Replace(ctx, s, fresh, opts...)
}

// PreserveServerFields copies Den's server-owned fields (_id, _created_at,
// _updated_at, _rev, and the soft-delete audit fields) from src onto dst,
// leaving client-owned fields untouched. The building block behind Replace,
// for callers that load and persist the existing record themselves.
func PreserveServerFields[T any](db *DB, dst, src *T) error {
	return engine.PreserveServerFields(db, dst, src)
}

// IsChanged reports whether the document has changed since it was loaded.
// Returns false if the document has no snapshot (never loaded or not Trackable).
func IsChanged[T any](db *DB, doc *T) (bool, error) {
	return engine.IsChanged(db, doc)
}

// GetChanges returns a map of field names to their before/after values
// for all fields that changed since the document was loaded.
func GetChanges[T any](db *DB, doc *T) (map[string]FieldChange, error) {
	return engine.GetChanges(db, doc)
}

// Revert restores the document to its state at load time by decoding the
// stored snapshot back over its fields. Returns ErrNoSnapshot if the
// document was never loaded or does not embed `document.Tracked`.
func Revert[T any](db *DB, doc *T) error {
	return engine.Revert(db, doc)
}

// NewLink creates a Link from an existing document, extracting its ID
// from the embedded `document.Base`.
func NewLink[T any](doc *T) Link[T] {
	return engine.NewLink(doc)
}

// FetchLink resolves a single named link field on a document.
func FetchLink[T any](ctx context.Context, s Scope, doc *T, fieldName string) error {
	return engine.FetchLink(ctx, s, doc, fieldName)
}

// FetchLinkField resolves the link by typed pointer instead of a
// stringly-named field on the parent.
func FetchLinkField[T any](ctx context.Context, s Scope, link *Link[T]) error {
	return engine.FetchLinkField(ctx, s, link)
}

// FetchAllLinks resolves the direct link fields on doc — single-level,
// the loaded targets' own links stay untouched.
func FetchAllLinks[T any](ctx context.Context, s Scope, doc *T) error {
	return engine.FetchAllLinks(ctx, s, doc)
}
