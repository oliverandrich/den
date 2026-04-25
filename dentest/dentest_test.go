// SPDX-License-Identifier: MIT

package dentest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
)

type dentestProbe struct {
	document.Base
	Name string `json:"name"`
}

func TestMustOpen_ReturnsUsableSQLite(t *testing.T) {
	db := dentest.MustOpen(t, &dentestProbe{})
	require.NotNil(t, db)

	ctx := context.Background()
	doc := &dentestProbe{Name: "hello"}
	require.NoError(t, den.Insert(ctx, db, doc))

	found, err := den.FindByID[dentestProbe](ctx, db, doc.ID)
	require.NoError(t, err)
	assert.Equal(t, "hello", found.Name)
}

func TestMustOpen_NoTypes(t *testing.T) {
	// Calling without any types must not panic and must not call
	// Register — covers the `len(types) > 0` short-circuit branch.
	db := dentest.MustOpen(t)
	require.NotNil(t, db)
	assert.Empty(t, den.Collections(db))
}

func TestMustOpenPostgres_ReturnsUsablePostgres(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &dentestProbe{})
	require.NotNil(t, db)

	ctx := context.Background()
	doc := &dentestProbe{Name: "hello-pg"}
	require.NoError(t, den.Insert(ctx, db, doc))
}

func TestMustOpenPostgresDefault_OpensViaPostgresURL(t *testing.T) {
	// Exercises the convenience wrapper that defaults the connection
	// string to PostgresURL(). The underlying open is identical to
	// MustOpenPostgres; this test pins the helper exists and works.
	db := dentest.MustOpenPostgresDefault(t, &dentestProbe{})
	require.NotNil(t, db)
	require.NoError(t, den.Insert(context.Background(), db, &dentestProbe{Name: "default"}))
}

func TestPostgresURL_FallbackWhenEnvUnset(t *testing.T) {
	t.Setenv("DEN_POSTGRES_URL", "")
	assert.Equal(t, "postgres://localhost/den_test", dentest.PostgresURL())
}

func TestPostgresURL_UsesEnvWhenSet(t *testing.T) {
	t.Setenv("DEN_POSTGRES_URL", "postgres://example.test/db?sslmode=disable")
	assert.Equal(t, "postgres://example.test/db?sslmode=disable", dentest.PostgresURL())
}
