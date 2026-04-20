package den_test

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
)

type Note struct {
	document.Base
	Title string `json:"title" den:"index"`
	Body  string `json:"body"`
}

type Category struct {
	document.Base
	Name string `json:"name" den:"unique"`
}

func TestMeta(t *testing.T) {
	db := dentest.MustOpen(t, &Note{})

	meta, err := den.Meta[Note](db)
	require.NoError(t, err)

	assert.Equal(t, "note", meta.Name)
	assert.False(t, meta.HasSoftDelete)
	assert.False(t, meta.HasRevision, "Note has no UseRevision setting")

	// Should have fields from Base + Note
	assert.GreaterOrEqual(t, len(meta.Fields), 5) // _id, _created_at, _updated_at, title, body

	// Find title field
	var titleField *den.FieldMeta
	for i := range meta.Fields {
		if meta.Fields[i].Name == "title" {
			titleField = &meta.Fields[i]
			break
		}
	}
	require.NotNil(t, titleField)
	assert.True(t, titleField.Indexed)
	assert.Equal(t, "string", titleField.Type)

	// Should have an index for title
	require.Len(t, meta.Indexes, 1)
	assert.Equal(t, "idx_note_title", meta.Indexes[0].Name)
	assert.False(t, meta.Indexes[0].Unique)
}

func TestMeta_Flags_SoftDeleteOnly(t *testing.T) {
	db := dentest.MustOpen(t, &SoftProduct{})
	meta, err := den.Meta[SoftProduct](db)
	require.NoError(t, err)
	assert.True(t, meta.HasSoftDelete)
	assert.False(t, meta.HasRevision)
}

func TestMeta_Flags_RevisionOnly(t *testing.T) {
	db := dentest.MustOpen(t, &RevProduct{})
	meta, err := den.Meta[RevProduct](db)
	require.NoError(t, err)
	assert.False(t, meta.HasSoftDelete)
	assert.True(t, meta.HasRevision)
}

func TestMeta_Flags_BothSoftDeleteAndRevision(t *testing.T) {
	db := dentest.MustOpen(t, &SoftRevProduct{})
	meta, err := den.Meta[SoftRevProduct](db)
	require.NoError(t, err)
	assert.True(t, meta.HasSoftDelete)
	assert.True(t, meta.HasRevision)
}

func TestMeta_Unregistered(t *testing.T) {
	db := dentest.MustOpen(t)

	_, err := den.Meta[Note](db)
	assert.ErrorIs(t, err, den.ErrNotRegistered)
}

func TestCollections(t *testing.T) {
	db := dentest.MustOpen(t, &Note{}, &Category{})

	names := den.Collections(db)
	sort.Strings(names)

	assert.Equal(t, []string{"category", "note"}, names)
}

func TestCollections_Empty(t *testing.T) {
	db := dentest.MustOpen(t)

	names := den.Collections(db)
	assert.Empty(t, names)
}

type CustomNameDoc struct {
	document.Base
	Title string `json:"title"`
}

func (d CustomNameDoc) DenSettings() den.Settings {
	return den.Settings{CollectionName: "custom_docs"}
}

// DocWithSQLInjectionTag carries an intentionally malicious json tag so we
// can verify Register rejects it at registration time before the name ever
// reaches SQL construction. staticcheck (SA5008) flags the tag as invalid —
// that is exactly what we're testing.
//
//nolint:staticcheck
type DocWithSQLInjectionTag struct {
	document.Base
	X string `json:"name';DROP TABLE foo;--"`
}

//nolint:staticcheck
type DocWithSingleQuoteTag struct {
	document.Base
	X string `json:"a'b"`
}

//nolint:staticcheck
type DocWithDoubleQuoteTag struct {
	document.Base
	X string `json:"a\"b"`
}

func TestRegister_RejectsInjectionInFieldName_SQLite(t *testing.T) {
	cases := []struct {
		name string
		doc  any
	}{
		{"semicolon injection", &DocWithSQLInjectionTag{}},
		{"single quote", &DocWithSingleQuoteTag{}},
		{"double quote", &DocWithDoubleQuoteTag{}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			db := dentest.MustOpen(t) // no types, so Register can fail
			err := den.Register(context.Background(), db, tt.doc)
			require.ErrorIs(t, err, den.ErrValidation,
				"Register must reject malicious field names with ErrValidation")
		})
	}
}

func TestRegister_RejectsInjectionInFieldName_Postgres(t *testing.T) {
	cases := []struct {
		name string
		doc  any
	}{
		{"semicolon injection", &DocWithSQLInjectionTag{}},
		{"single quote", &DocWithSingleQuoteTag{}},
		{"double quote", &DocWithDoubleQuoteTag{}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			db := dentest.MustOpenPostgres(t, dentest.PostgresURL())
			err := den.Register(context.Background(), db, tt.doc)
			require.ErrorIs(t, err, den.ErrValidation,
				"Register must reject malicious field names with ErrValidation")
		})
	}
}

func TestRegister_CustomCollectionName(t *testing.T) {
	db := dentest.MustOpen(t, &CustomNameDoc{})
	ctx := context.Background()

	// Meta should reflect the custom name
	meta, err := den.Meta[CustomNameDoc](db)
	require.NoError(t, err)
	assert.Equal(t, "custom_docs", meta.Name)

	// CRUD should work with the custom name
	doc := &CustomNameDoc{Title: "Hello"}
	require.NoError(t, den.Insert(ctx, db, doc))
	assert.NotEmpty(t, doc.ID)

	found, err := den.FindByID[CustomNameDoc](ctx, db, doc.ID)
	require.NoError(t, err)
	assert.Equal(t, "Hello", found.Title)

	// Collections should list the custom name
	names := den.Collections(db)
	assert.Contains(t, names, "custom_docs")
}

// -- Composite index tests --

type CompositeUniqueDoc struct {
	document.Base
	UserID string `json:"user_id" den:"unique_together:user_name"`
	Name   string `json:"name" den:"unique_together:user_name"`
}

type CompositeIndexDoc struct {
	document.Base
	FeedID string `json:"feed_id" den:"index_together:feed_date"`
	Date   string `json:"date" den:"index_together:feed_date"`
}

type SettingsIndexDoc struct {
	document.Base
	TenantID string `json:"tenant_id"`
	Email    string `json:"email"`
}

func (d SettingsIndexDoc) DenSettings() den.Settings {
	return den.Settings{
		Indexes: []den.IndexDefinition{{
			Name:   "idx_settingsindexdoc_tenant_email",
			Fields: []string{"tenant_id", "email"},
			Unique: true,
		}},
	}
}

func TestRegister_CompositeUniqueIndex(t *testing.T) {
	db := dentest.MustOpen(t, &CompositeUniqueDoc{})

	meta, err := den.Meta[CompositeUniqueDoc](db)
	require.NoError(t, err)

	// Should have one composite unique index
	var found *den.IndexDefinition
	for i := range meta.Indexes {
		if meta.Indexes[i].Name == "idx_compositeuniquedoc_user_name" {
			found = &meta.Indexes[i]
			break
		}
	}
	require.NotNil(t, found, "composite unique index should exist")
	assert.True(t, found.Unique)
	assert.Equal(t, []string{"user_id", "name"}, found.Fields)
}

func TestRegister_CompositeUniqueIndex_EnforcesDuplicates(t *testing.T) {
	db := dentest.MustOpen(t, &CompositeUniqueDoc{})
	ctx := context.Background()

	doc1 := &CompositeUniqueDoc{UserID: "user1", Name: "alice"}
	require.NoError(t, den.Insert(ctx, db, doc1))

	// Same composite key → ErrDuplicate
	doc2 := &CompositeUniqueDoc{UserID: "user1", Name: "alice"}
	err := den.Insert(ctx, db, doc2)
	require.ErrorIs(t, err, den.ErrDuplicate)

	// Different user, same name → OK
	doc3 := &CompositeUniqueDoc{UserID: "user2", Name: "alice"}
	require.NoError(t, den.Insert(ctx, db, doc3))

	// Same user, different name → OK
	doc4 := &CompositeUniqueDoc{UserID: "user1", Name: "bob"}
	require.NoError(t, den.Insert(ctx, db, doc4))
}

func TestRegister_CompositeNonUniqueIndex(t *testing.T) {
	db := dentest.MustOpen(t, &CompositeIndexDoc{})

	meta, err := den.Meta[CompositeIndexDoc](db)
	require.NoError(t, err)

	var found *den.IndexDefinition
	for i := range meta.Indexes {
		if meta.Indexes[i].Name == "idx_compositeindexdoc_feed_date" {
			found = &meta.Indexes[i]
			break
		}
	}
	require.NotNil(t, found, "composite non-unique index should exist")
	assert.False(t, found.Unique)
	assert.Equal(t, []string{"feed_id", "date"}, found.Fields)
}

func TestRegister_SettingsIndexes(t *testing.T) {
	db := dentest.MustOpen(t, &SettingsIndexDoc{})
	ctx := context.Background()

	meta, err := den.Meta[SettingsIndexDoc](db)
	require.NoError(t, err)

	// Should have the custom composite unique index from Settings
	var found *den.IndexDefinition
	for i := range meta.Indexes {
		if meta.Indexes[i].Name == "idx_settingsindexdoc_tenant_email" {
			found = &meta.Indexes[i]
			break
		}
	}
	require.NotNil(t, found, "settings-defined composite index should exist")
	assert.True(t, found.Unique)
	assert.Equal(t, []string{"tenant_id", "email"}, found.Fields)

	// Enforce uniqueness
	doc1 := &SettingsIndexDoc{TenantID: "t1", Email: "a@b.com"}
	require.NoError(t, den.Insert(ctx, db, doc1))

	doc2 := &SettingsIndexDoc{TenantID: "t1", Email: "a@b.com"}
	err = den.Insert(ctx, db, doc2)
	require.ErrorIs(t, err, den.ErrDuplicate)
}

func TestPing(t *testing.T) {
	db := dentest.MustOpen(t, &Note{})
	err := db.Ping(context.Background())
	assert.NoError(t, err)
}

func TestBackendAccessor(t *testing.T) {
	db := dentest.MustOpen(t, &Note{})
	assert.NotNil(t, db.Backend())
}

// PtrSettingsDoc defines DenSettings on a pointer receiver. When the user
// passes a VALUE to Register (instead of a pointer), the straight type-
// assertion against DenSettable fails because only *PtrSettingsDoc has the
// method in its method set. getSettings must detect this case and retry
// via a synthesized pointer so the settings are not silently ignored.
type PtrSettingsDoc struct {
	document.Base
	Name string `json:"name"`
}

func (d *PtrSettingsDoc) DenSettings() den.Settings {
	return den.Settings{CollectionName: "ptr_settings_custom"}
}

func TestRegister_ValueWithPointerReceiverSettings(t *testing.T) {
	db := dentest.MustOpen(t)
	ctx := context.Background()

	require.NoError(t, den.Register(ctx, db, PtrSettingsDoc{}))

	meta, err := den.Meta[PtrSettingsDoc](db)
	require.NoError(t, err)
	assert.Equal(t, "ptr_settings_custom", meta.Name)
}

func TestOpenURL_WithTypes(t *testing.T) {
	ctx := context.Background()
	dsn := "sqlite:///" + t.TempDir() + "/with_types.db"
	db, err := den.OpenURL(ctx, dsn, den.WithTypes(&Product{}, &Note{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cols := den.Collections(db)
	assert.Contains(t, cols, "product")
	assert.Contains(t, cols, "note")

	require.NoError(t, den.Insert(ctx, db, &Product{Name: "W", Price: 1.0}))
}

func TestOpenURL_WithTypes_PropagatesRegistrationError(t *testing.T) {
	dsn := "sqlite:///" + t.TempDir() + "/bad_types.db"
	_, err := den.OpenURL(context.Background(), dsn, den.WithTypes(&DocWithSQLInjectionTag{}))
	require.Error(t, err)
	require.ErrorIs(t, err, den.ErrValidation)
}

// TestOpenURL_ContextCanceledDuringRegistration regresses that OpenURL
// honors the passed context when WithTypes triggers collection/index
// provisioning — a canceled context should abort the setup cleanly
// instead of silently running to completion on context.Background().
func TestOpenURL_ContextCanceledDuringRegistration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before any work starts

	dsn := "sqlite:///" + t.TempDir() + "/canceled.db"
	_, err := den.OpenURL(ctx, dsn, den.WithTypes(&Product{}))
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}
