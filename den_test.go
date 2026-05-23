// Smoke tests for the wrappers in den.go. Like the other *_test.go files
// in the den root, these exist so the root package shows non-zero
// coverage — without them no other test file imports `den`. The engine
// logic each wrapper delegates to is tested against `engine.X` in
// engine's own test suite.

package den_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
)

// Shared fixture types used across crud_test.go, query_test.go, and
// options_test.go too. Declared here so the den_test package has a single
// home for them.

type smokeAuthor struct {
	document.Base
	document.Tracked
	Name string `json:"name" den:"index"`
}

type smokeBook struct {
	document.Base
	document.SoftDelete
	Title  string                `json:"title" den:"index"`
	Author den.Link[smokeAuthor] `json:"author"`
}

func TestDen_NewID(t *testing.T) {
	id := den.NewID()
	assert.Len(t, id, 26, "ULID strings are 26 Crockford base32 characters")
}

func TestDen_Meta(t *testing.T) {
	db := dentest.MustOpen(t, &smokeAuthor{})
	meta, err := den.Meta[smokeAuthor](db)
	require.NoError(t, err)
	assert.NotEmpty(t, meta.Name)
}

func TestDen_Open(t *testing.T) {
	db := dentest.MustOpen(t, &smokeAuthor{})
	ctx := context.Background()

	// Open with a Backend obtained from an existing DB.
	manual, err := den.Open(ctx, db.Backend())
	require.NoError(t, err)
	require.NotNil(t, manual)
}

func TestDen_DropStaleIndexes(t *testing.T) {
	db := dentest.MustOpen(t, &smokeAuthor{})
	ctx := context.Background()

	// DryRun — no schema change actually fires.
	res, err := den.DropStaleIndexes(ctx, db, den.DryRun())
	require.NoError(t, err)
	_ = res // result shape covered by engine tests.
}
