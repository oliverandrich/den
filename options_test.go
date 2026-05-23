// Smoke tests for the option constructors in options.go (CRUDOption /
// LockOption / Option). See den_test.go for shared fixture types and
// the rationale for these tests.

package den_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
)

// TestOptions_Constructors pins every option constructor to non-nil so
// the option-constructor route hits the wrapper at least once.
func TestOptions_Constructors(t *testing.T) {
	// CRUDOptions.
	assert.NotNil(t, den.WithLinkRule(den.LinkWrite))
	assert.NotNil(t, den.WithoutFetchLinks())
	assert.NotNil(t, den.HardDelete())
	assert.NotNil(t, den.IncludeDeleted())
	assert.NotNil(t, den.SoftDeleteBy("usr-1"))
	assert.NotNil(t, den.SoftDeleteReason("cleanup"))
	assert.NotNil(t, den.IgnoreRevision())

	// LockOptions.
	assert.NotNil(t, den.NoWait())
	assert.NotNil(t, den.SkipLocked())

	// Open options.
	assert.NotNil(t, den.WithTypes(&smokeAuthor{}))
	assert.NotNil(t, den.WithStorage(nil)) // nil Storage is valid syntactically; never opens.

	// DropStaleOption.
	assert.NotNil(t, den.DryRun())
}

// TestOptions_AppliedToCRUD drives a few CRUD options through real calls
// so the application path also gets coverage.
func TestOptions_AppliedToCRUD(t *testing.T) {
	db := dentest.MustOpen(t, &smokeBook{})
	ctx := context.Background()

	b := &smokeBook{Title: "Option doc"}
	require.NoError(t, den.Save(ctx, db, b))

	// HardDelete bypasses the soft-delete path on a SoftDelete doc.
	require.NoError(t, den.Delete(ctx, db, b, den.HardDelete()))

	// FindByID with IncludeDeleted + WithoutFetchLinks — confirms the
	// option round-trip even though the row is gone for good.
	_, err := den.FindByID[smokeBook](ctx, db, b.ID, den.IncludeDeleted(), den.WithoutFetchLinks())
	require.ErrorIs(t, err, den.ErrNotFound)
}
