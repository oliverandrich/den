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
	assert.False(t, meta.HasSoftBase)

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

func TestPing(t *testing.T) {
	db := dentest.MustOpen(t, &Note{})
	err := db.Ping(context.Background())
	assert.NoError(t, err)
}

func TestBackendAccessor(t *testing.T) {
	db := dentest.MustOpen(t, &Note{})
	assert.NotNil(t, db.Backend())
}
