package core_test

import (
	"context"
	"testing"

	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/internal/core"
	"github.com/oliverandrich/den/where"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types for den-1351 (nested struct fields with den: tags). The walker
// must recurse into named struct fields and pointer-to-struct fields, and
// the synthesised indexes must use SQL-safe names while keeping the
// dotted JSON path for backends to translate.

type nestedUserProfile struct {
	Slug       string `json:"slug"                 den:"unique"`
	Department string `json:"department,omitempty" den:"index"`
	Bio        string `json:"bio"`
}

type nestedUser struct {
	document.Base
	Email   string            `json:"email" den:"index"`
	Profile nestedUserProfile `json:"profile"`
}

type nestedUserPointer struct {
	document.Base
	Email   string             `json:"email" den:"index"`
	Profile *nestedUserProfile `json:"profile,omitempty"`
}

type nestedZip struct {
	Zip string `json:"zip" den:"index"`
}

type nestedCity struct {
	City nestedZip `json:"city"`
}

type nestedDeepDoc struct {
	document.Base
	Name string     `json:"name"`
	Addr nestedCity `json:"addr"`
}

func TestNestedField_UniqueRejectsDuplicate(t *testing.T) {
	db := dentest.MustOpen(t, &nestedUser{})
	ctx := context.Background()

	first := &nestedUser{Email: "a@example.com", Profile: nestedUserProfile{Slug: "ada"}}
	require.NoError(t, core.Save(ctx, db, first))

	second := &nestedUser{Email: "b@example.com", Profile: nestedUserProfile{Slug: "ada"}}
	err := core.Save(ctx, db, second)
	require.Error(t, err, "duplicate nested Profile.Slug must be rejected")
	assert.ErrorIs(t, err, core.ErrDuplicate)
}

func TestNestedField_IndexIsRegistered(t *testing.T) {
	db := dentest.MustOpen(t, &nestedUser{})

	meta, err := core.Meta[nestedUser](db)
	require.NoError(t, err)

	byName := make(map[string]core.IndexDefinition, len(meta.Indexes))
	for _, idx := range meta.Indexes {
		byName[idx.Name] = idx
	}

	slug, ok := byName["idx_nesteduser_profile_slug"]
	require.True(t, ok, "unique index on nested Profile.Slug must use a SQL-safe identifier")
	assert.True(t, slug.Unique)
	// Dotted path is preserved in Fields so the backend can build
	// json_extract(data, '$.profile.slug').
	assert.Equal(t, []string{"profile.slug"}, slug.Fields)

	dept, ok := byName["idx_nesteduser_profile_department"]
	require.True(t, ok, "index on nested Profile.Department must use a SQL-safe identifier")
	assert.False(t, dept.Unique)
	assert.Equal(t, []string{"profile.department"}, dept.Fields)
}

func TestNestedField_NilPointerSurvivesRegisterAndCRUD(t *testing.T) {
	db := dentest.MustOpen(t, &nestedUserPointer{})
	ctx := context.Background()

	// Nil Profile must not break Register, Save, or FindByID.
	doc := &nestedUserPointer{Email: "x@example.com"}
	require.NoError(t, core.Save(ctx, db, doc))

	found, err := core.FindByID[nestedUserPointer](ctx, db, doc.ID)
	require.NoError(t, err)
	assert.Equal(t, "x@example.com", found.Email)
	assert.Nil(t, found.Profile)

	// Two nil-pointer profiles are not "duplicate" — unique indexes on
	// nullable columns allow multiple NULLs.
	other := &nestedUserPointer{Email: "y@example.com"}
	require.NoError(t, core.Save(ctx, db, other))
}

func TestNestedField_PointerToStructUniqueRejectsDuplicate(t *testing.T) {
	db := dentest.MustOpen(t, &nestedUserPointer{})
	ctx := context.Background()

	first := &nestedUserPointer{Email: "a@example.com", Profile: &nestedUserProfile{Slug: "ada"}}
	require.NoError(t, core.Save(ctx, db, first))

	dup := &nestedUserPointer{Email: "b@example.com", Profile: &nestedUserProfile{Slug: "ada"}}
	err := core.Save(ctx, db, dup)
	require.Error(t, err)
	assert.ErrorIs(t, err, core.ErrDuplicate)
}

func TestNestedField_TwoLevelsDeepIndex(t *testing.T) {
	db := dentest.MustOpen(t, &nestedDeepDoc{})
	ctx := context.Background()

	require.NoError(t, core.SaveAll(ctx, db, []*nestedDeepDoc{
		{Name: "alice", Addr: nestedCity{City: nestedZip{Zip: "10115"}}},
		{Name: "bob", Addr: nestedCity{City: nestedZip{Zip: "10115"}}},
		{Name: "carol", Addr: nestedCity{City: nestedZip{Zip: "20457"}}},
	}))

	results, err := core.NewQuery[nestedDeepDoc](db, where.Field("addr.city.zip").Eq("10115")).All(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 2)

	meta, err := core.Meta[nestedDeepDoc](db)
	require.NoError(t, err)
	var found bool
	for _, idx := range meta.Indexes {
		if idx.Name == "idx_nesteddeepdoc_addr_city_zip" {
			found = true
			assert.Equal(t, []string{"addr.city.zip"}, idx.Fields)
		}
	}
	assert.True(t, found, "depth-3 index name must be derived from the dotted path")
}

func TestNestedField_SetFieldsOnValueStructWorks(t *testing.T) {
	db := dentest.MustOpen(t, &nestedUser{})
	ctx := context.Background()

	doc := &nestedUser{Email: "a@example.com", Profile: nestedUserProfile{Slug: "ada", Bio: "old"}}
	require.NoError(t, core.Save(ctx, db, doc))

	updated, err := core.NewQuery[nestedUser](db, where.Field("_id").Eq(doc.ID)).
		UpdateOne(ctx, core.SetFields{"profile.bio": "new"})
	require.NoError(t, err)
	assert.Equal(t, "new", updated.Profile.Bio)
	assert.Equal(t, "ada", updated.Profile.Slug, "non-targeted nested fields are preserved")
}

func TestNestedField_SetFieldsThroughNilPointerErrors(t *testing.T) {
	db := dentest.MustOpen(t, &nestedUserPointer{})
	ctx := context.Background()

	doc := &nestedUserPointer{Email: "a@example.com"} // nil Profile
	require.NoError(t, core.Save(ctx, db, doc))

	_, err := core.NewQuery[nestedUserPointer](db, where.Field("_id").Eq(doc.ID)).
		UpdateOne(ctx, core.SetFields{"profile.bio": "new"})
	require.Error(t, err, "setting a nested field through a nil pointer should error, not panic")
	// The reflect error chain wraps something like "indirection through nil pointer".
	// Surface it via the field-qualified message so callers know which name failed.
	assert.Contains(t, err.Error(), "profile.bio")
}

// TestNestedFieldParity_UniqueRejectsDuplicate exercises the nested-
// unique behaviour on both backends. Lifted from a SQLite-only test
// once den-8f8t taught the Postgres schema DDL to translate dotted
// paths into jsonb_extract_path_text.
func TestNestedFieldParity_UniqueRejectsDuplicate(t *testing.T) {
	for name, openDB := range map[string]func(*testing.T) *core.DB{
		"sqlite":   func(t *testing.T) *core.DB { return dentest.MustOpen(t, &nestedUser{}) },
		"postgres": func(t *testing.T) *core.DB { return dentest.MustOpenPostgresDefault(t, &nestedUser{}) },
	} {
		t.Run(name, func(t *testing.T) {
			db := openDB(t)
			ctx := context.Background()

			first := &nestedUser{Email: "a@example.com", Profile: nestedUserProfile{Slug: "ada"}}
			require.NoError(t, core.Save(ctx, db, first))

			dup := &nestedUser{Email: "b@example.com", Profile: nestedUserProfile{Slug: "ada"}}
			err := core.Save(ctx, db, dup)
			require.Error(t, err)
			assert.ErrorIs(t, err, core.ErrDuplicate)
		})
	}
}

// Nested FTS test types — round-trip a Profile.Bio FTS field on both
// backends to pin the den-ciug behaviour.

type nestedFTSProfile struct {
	Slug string `json:"slug"`
	Bio  string `json:"bio" den:"fts"`
}

type nestedFTSUser struct {
	document.Base
	Email   string           `json:"email"`
	Profile nestedFTSProfile `json:"profile"`
}

func TestNestedFieldParity_FTSOnNestedField(t *testing.T) {
	for name, openDB := range map[string]func(*testing.T) *core.DB{
		"sqlite":   func(t *testing.T) *core.DB { return dentest.MustOpen(t, &nestedFTSUser{}) },
		"postgres": func(t *testing.T) *core.DB { return dentest.MustOpenPostgresDefault(t, &nestedFTSUser{}) },
	} {
		t.Run(name, func(t *testing.T) {
			db := openDB(t)
			ctx := context.Background()

			require.NoError(t, core.SaveAll(ctx, db, []*nestedFTSUser{
				{Email: "a@example.com", Profile: nestedFTSProfile{Slug: "ada", Bio: "lovelace numerical engine"}},
				{Email: "b@example.com", Profile: nestedFTSProfile{Slug: "alan", Bio: "turing computing machinery"}},
				{Email: "c@example.com", Profile: nestedFTSProfile{Slug: "grace", Bio: "hopper compilers and cobol"}},
			}))

			results, err := core.NewQuery[nestedFTSUser](db).Search(ctx, "lovelace")
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, "ada", results[0].Profile.Slug)

			results, err = core.NewQuery[nestedFTSUser](db).Search(ctx, "compilers")
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, "grace", results[0].Profile.Slug)
		})
	}
}
