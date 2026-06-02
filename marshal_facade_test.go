// Smoke tests for the link output/introspection wrappers in den.go
// (Marshal, LinkFields). See den_test.go for the shared fixture types.

package den_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
)

func TestFacade_MarshalExpandsLoadedLinks(t *testing.T) {
	db := dentest.MustOpen(t, &smokeAuthor{}, &smokeBook{})
	ctx := context.Background()

	author := &smokeAuthor{Name: "Ursula"}
	require.NoError(t, den.Save(ctx, db, author))
	book := &smokeBook{Title: "Earthsea", Author: den.NewLink(author)}
	require.NoError(t, den.Save(ctx, db, book))

	// Hydrate, then Marshal: the author link expands to an object.
	got, err := den.NewQuery[smokeBook](db).WithFetchLinks("author").First(ctx)
	require.NoError(t, err)
	out, err := den.Marshal(got)
	require.NoError(t, err)

	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &m))
	var auth map[string]any
	require.NoError(t, json.Unmarshal(m["author"], &auth))
	assert.Equal(t, "Ursula", auth["name"], "den.Marshal expands the loaded link")

	// json.Marshal stays the bare id — default wire format unchanged.
	std, err := json.Marshal(got)
	require.NoError(t, err)
	assert.Contains(t, string(std), `"author":"`+author.ID+`"`)
}

func TestFacade_LinkFields(t *testing.T) {
	db := dentest.MustOpen(t, &smokeAuthor{}, &smokeBook{})
	lfs, err := den.LinkFields[smokeBook](db)
	require.NoError(t, err)
	require.Len(t, lfs, 1)
	assert.Equal(t, "author", lfs[0].JSONName)
	assert.Equal(t, "smokeauthor", lfs[0].TargetCollection)
	assert.False(t, lfs[0].Slice)
}
