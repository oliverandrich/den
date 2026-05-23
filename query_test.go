// Smoke tests for the wrappers in query.go (NewQuery, RunInTransaction,
// LockByID, AdvisoryLock). See den_test.go for shared fixture types and
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

// TestQuery_NewQuery exercises the chainable query entry point and the
// Asc/Desc sort constants.
func TestQuery_NewQuery(t *testing.T) {
	db := dentest.MustOpen(t, &smokeAuthor{})
	ctx := context.Background()

	for _, name := range []string{"Ada", "Grace", "Margaret"} {
		require.NoError(t, den.Save(ctx, db, &smokeAuthor{Name: name}))
	}

	all, err := den.NewQuery[smokeAuthor](db).Sort("name", den.Asc).All(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

// TestQuery_Transactions covers RunInTransaction, LockByID, AdvisoryLock.
// SQLite's locking is coarse-grained but the wrappers still route through.
func TestQuery_Transactions(t *testing.T) {
	db := dentest.MustOpen(t, &smokeAuthor{})
	ctx := context.Background()

	a := &smokeAuthor{Name: "Tx target"}
	require.NoError(t, den.Save(ctx, db, a))

	require.NoError(t, den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		// LockByID — SQLite no-ops on row locks, but the wrapper still routes.
		locked, err := den.LockByID[smokeAuthor](ctx, tx, a.ID)
		if err != nil {
			return err
		}
		if locked.Name != "Tx target" {
			t.Errorf("locked doc has unexpected name: %q", locked.Name)
		}

		// AdvisoryLock — SQLite no-ops; the wrapper still routes.
		return den.AdvisoryLock(ctx, tx, 42)
	}))
}
