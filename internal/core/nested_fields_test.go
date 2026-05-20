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

// runOnBothBackends runs body against a SQLite and a Postgres database
// for the given document type. Used by every parity-shaped test in this
// file; collapses the per-test sqlite/postgres open-loop boilerplate.
func runOnBothBackends(t *testing.T, doc document.Document, body func(*testing.T, *core.DB)) {
	t.Helper()
	for name, openDB := range map[string]func(*testing.T) *core.DB{
		"sqlite":   func(t *testing.T) *core.DB { return dentest.MustOpen(t, doc) },
		"postgres": func(t *testing.T) *core.DB { return dentest.MustOpenPostgresDefault(t, doc) },
	} {
		t.Run(name, func(t *testing.T) {
			body(t, openDB(t))
		})
	}
}

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
	runOnBothBackends(t, &nestedUser{}, func(t *testing.T, db *core.DB) {
		ctx := context.Background()

		first := &nestedUser{Email: "a@example.com", Profile: nestedUserProfile{Slug: "ada"}}
		require.NoError(t, core.Save(ctx, db, first))

		dup := &nestedUser{Email: "b@example.com", Profile: nestedUserProfile{Slug: "ada"}}
		err := core.Save(ctx, db, dup)
		require.ErrorIs(t, err, core.ErrDuplicate)
	})
}

// Composite unique_together / index_together that spans a flat field
// and a nested field — pinned end-to-end on both backends. The walker
// puts both fields in the same group; backends materialise the index
// with mixed flat and dotted JSON-path expressions.

type compositeNestedInner struct {
	Slug string `json:"slug" den:"unique_together:tenant_slug,index_together:tenant_dept"`
	Dept string `json:"dept" den:"index_together:tenant_dept"`
}

type compositeNestedDoc struct {
	document.Base
	TenantID string               `json:"tenant_id" den:"unique_together:tenant_slug,index_together:tenant_dept"`
	Profile  compositeNestedInner `json:"profile"`
}

func TestNestedField_CompositeUniqueTogetherMetaSpansLevels(t *testing.T) {
	db := dentest.MustOpen(t, &compositeNestedDoc{})

	meta, err := core.Meta[compositeNestedDoc](db)
	require.NoError(t, err)

	byName := make(map[string]core.IndexDefinition, len(meta.Indexes))
	for _, idx := range meta.Indexes {
		byName[idx.Name] = idx
	}

	uniq, ok := byName["idx_compositenesteddoc_tenant_slug"]
	require.True(t, ok, "unique_together group must materialise as a composite index")
	assert.True(t, uniq.Unique)
	assert.ElementsMatch(t, []string{"tenant_id", "profile.slug"}, uniq.Fields,
		"composite unique must contain both the flat and nested field with the dotted path")

	multi, ok := byName["idx_compositenesteddoc_tenant_dept"]
	require.True(t, ok, "index_together group must materialise as a composite index")
	assert.False(t, multi.Unique)
	assert.ElementsMatch(t, []string{"tenant_id", "profile.slug", "profile.dept"}, multi.Fields)
}

func TestNestedFieldParity_CompositeUniqueTogetherRejectsCollision(t *testing.T) {
	runOnBothBackends(t, &compositeNestedDoc{}, func(t *testing.T, db *core.DB) {
		ctx := context.Background()

		first := &compositeNestedDoc{TenantID: "t1", Profile: compositeNestedInner{Slug: "ada", Dept: "eng"}}
		require.NoError(t, core.Save(ctx, db, first))

		// Same tenant + same nested slug → composite unique violation.
		dup := &compositeNestedDoc{TenantID: "t1", Profile: compositeNestedInner{Slug: "ada", Dept: "sales"}}
		err := core.Save(ctx, db, dup)
		require.ErrorIs(t, err, core.ErrDuplicate)

		// Different tenant, same slug → allowed.
		other := &compositeNestedDoc{TenantID: "t2", Profile: compositeNestedInner{Slug: "ada", Dept: "eng"}}
		require.NoError(t, core.Save(ctx, db, other))
	})
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

// Composite FTS doc — one flat FTS field + one nested FTS field. Pins
// that ensureFTSForCollection collects both into the same backend FTS
// surface (SQLite: two columns in one virtual table; PG: one tsvector
// generated column concatenating both expressions).

type nestedFTSMixedDoc struct {
	document.Base
	Title   string           `json:"title" den:"fts"`
	Profile nestedFTSProfile `json:"profile"`
}

func TestNestedFieldParity_FTSCompositeFlatAndNested(t *testing.T) {
	runOnBothBackends(t, &nestedFTSMixedDoc{}, func(t *testing.T, db *core.DB) {
		ctx := context.Background()

		require.NoError(t, core.SaveAll(ctx, db, []*nestedFTSMixedDoc{
			{Title: "engine internals", Profile: nestedFTSProfile{Slug: "ada", Bio: "designed mechanical computation"}},
			{Title: "lambda calculus", Profile: nestedFTSProfile{Slug: "alonzo", Bio: "founded recursion theory"}},
		}))

		// Match in the flat field.
		hits, err := core.NewQuery[nestedFTSMixedDoc](db).Search(ctx, "engine")
		require.NoError(t, err)
		require.Len(t, hits, 1)
		assert.Equal(t, "ada", hits[0].Profile.Slug)

		// Match in the nested field.
		hits, err = core.NewQuery[nestedFTSMixedDoc](db).Search(ctx, "recursion")
		require.NoError(t, err)
		require.Len(t, hits, 1)
		assert.Equal(t, "alonzo", hits[0].Profile.Slug)

		// Negative control: Slug carries no `den:"fts"`, so searching
		// for a unique slug value must NOT surface anything — proves
		// untagged sibling fields stay out of the FTS index.
		hits, err = core.NewQuery[nestedFTSMixedDoc](db).Search(ctx, "alonzo")
		require.NoError(t, err)
		assert.Empty(t, hits, "untagged nested field must not leak into the FTS surface")
	})
}

// Nil pointer-to-struct holding an FTS field — Save must not panic in
// the trigger / tsvector expression, and Search for a term that would
// only match the nil row must return nothing.

type nestedFTSPtrDoc struct {
	document.Base
	Email   string            `json:"email"`
	Profile *nestedFTSProfile `json:"profile,omitempty"`
}

func TestNestedFieldParity_FTSOnNilPointerSurvives(t *testing.T) {
	runOnBothBackends(t, &nestedFTSPtrDoc{}, func(t *testing.T, db *core.DB) {
		ctx := context.Background()

		// nil Profile — the FTS path resolves to NULL (json_extract) or
		// empty (coalesce in tsvector). Trigger / generated column must
		// accept it without error.
		require.NoError(t, core.Save(ctx, db, &nestedFTSPtrDoc{Email: "a@example.com"}))

		// A non-nil sibling so the FTS infrastructure has at least one
		// indexed row to scan.
		require.NoError(t, core.Save(ctx, db, &nestedFTSPtrDoc{
			Email:   "b@example.com",
			Profile: &nestedFTSProfile{Slug: "ada", Bio: "lovelace"},
		}))

		// Search hits only the populated row; the nil-profile row must
		// not surface and must not have crashed the trigger.
		hits, err := core.NewQuery[nestedFTSPtrDoc](db).Search(ctx, "lovelace")
		require.NoError(t, err)
		require.Len(t, hits, 1)
		assert.Equal(t, "b@example.com", hits[0].Email)
	})
}

func TestNestedFieldParity_FTSOnNestedField(t *testing.T) {
	runOnBothBackends(t, &nestedFTSUser{}, func(t *testing.T, db *core.DB) {
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

// Project + GroupBy on dotted JSON paths (den-tgy7). Project resolves
// the path through nested maps; GroupBy threads the dotted name into
// json_extract / jsonb_extract_path_text.

// projectNestedSummary deliberately mixes json:"…" (flat) and den:"from:…"
// (dotted) to also pin the den-first / json-fallback resolution chain
// in getProjMappings.
type projectNestedSummary struct {
	ID    string `json:"_id"`
	Email string `json:"email"`
	Slug  string `den:"from:profile.slug"`
	Bio   string `den:"from:profile.bio"`
}

type groupByNestedRow struct {
	Department string `den:"group_key"`
	Count      int    `den:"count"`
}

func TestNestedFieldParity_ProjectNestedPath(t *testing.T) {
	runOnBothBackends(t, &nestedUser{}, func(t *testing.T, db *core.DB) {
		ctx := context.Background()

		require.NoError(t, core.SaveAll(ctx, db, []*nestedUser{
			{Email: "a@example.com", Profile: nestedUserProfile{Slug: "ada", Department: "eng", Bio: "lovelace"}},
			{Email: "b@example.com", Profile: nestedUserProfile{Slug: "alan", Department: "sales", Bio: "turing"}},
		}))

		var out []projectNestedSummary
		require.NoError(t, core.NewQuery[nestedUser](db).Sort("email", core.Asc).Project(ctx, &out))
		require.Len(t, out, 2)
		assert.Equal(t, "ada", out[0].Slug, "Project resolves dotted JSON path via den:\"from:profile.slug\"")
		assert.Equal(t, "lovelace", out[0].Bio)
		assert.Equal(t, "alan", out[1].Slug)
		assert.Equal(t, "a@example.com", out[0].Email, "flat fields keep working in the same projection")
	})
}

func TestNestedFieldParity_GroupByNestedPath(t *testing.T) {
	runOnBothBackends(t, &nestedUser{}, func(t *testing.T, db *core.DB) {
		ctx := context.Background()

		require.NoError(t, core.SaveAll(ctx, db, []*nestedUser{
			{Email: "a@example.com", Profile: nestedUserProfile{Slug: "ada", Department: "eng"}},
			{Email: "b@example.com", Profile: nestedUserProfile{Slug: "alan", Department: "eng"}},
			{Email: "c@example.com", Profile: nestedUserProfile{Slug: "grace", Department: "sales"}},
		}))

		var rows []groupByNestedRow
		require.NoError(t,
			core.NewQuery[nestedUser](db).
				GroupBy("profile.department").
				Into(ctx, &rows),
		)
		require.Len(t, rows, 2)

		byDept := make(map[string]int, len(rows))
		for _, r := range rows {
			byDept[r.Department] = r.Count
		}
		assert.Equal(t, 2, byDept["eng"], "GroupBy on dotted path aggregates by the nested value")
		assert.Equal(t, 1, byDept["sales"])
	})
}
