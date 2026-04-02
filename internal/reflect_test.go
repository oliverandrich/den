package internal

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testBase struct {
	CreatedAt time.Time `json:"_created_at"`
	UpdatedAt time.Time `json:"_updated_at"`
	ID        string    `json:"_id"`
}

type simpleDoc struct {
	testBase
	Name  string  `json:"name"`
	Price float64 `json:"price" den:"index"`
}

type taggedDoc struct {
	testBase
	SKU   string `json:"sku" den:"unique"`
	Body  string `json:"body" den:"fts"`
	Plain string
	Tags  []string `json:"tags,omitempty" den:"index"`
}

type noTagDoc struct {
	testBase
	Title string
	Count int
}

type softBase struct {
	DeletedAt *time.Time `json:"_deleted_at,omitempty"`
	testBase
}

type softDoc struct {
	softBase
	Name string `json:"name"`
}

func TestParseDenTag(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		wantOpts TagOptions
	}{
		{"empty", "", TagOptions{}},
		{"index", "index", TagOptions{Index: true}},
		{"unique", "unique", TagOptions{Unique: true}},
		{"fts", "fts", TagOptions{FTS: true}},
		{"multiple", "index,fts", TagOptions{Index: true, FTS: true}},
		{"all", "index,unique,fts", TagOptions{Index: true, Unique: true, FTS: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := ParseDenTag(tt.tag)
			assert.Equal(t, tt.wantOpts, opts)
		})
	}
}

func TestParseJSONTagName(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{"name", "name"},
		{"name,omitempty", "name"},
		{"_id", "_id"},
		{"", ""},
		{"-", ""},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseJSONTagName(tt.tag))
		})
	}
}

func TestAnalyzeStruct(t *testing.T) {
	t.Run("simple document", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[simpleDoc]())
		require.NoError(t, err)

		assert.Equal(t, "simpledoc", info.CollectionName)
		require.Len(t, info.Fields, 5) // 3 from testBase + 2 own

		nameField := info.FieldByName("name")
		require.NotNil(t, nameField)
		assert.Equal(t, "name", nameField.JSONName)
		assert.False(t, nameField.Options.Index)

		priceField := info.FieldByName("price")
		require.NotNil(t, priceField)
		assert.Equal(t, "price", priceField.JSONName)
		assert.True(t, priceField.Options.Index)
	})

	t.Run("flattens embedded base fields", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[simpleDoc]())
		require.NoError(t, err)

		idField := info.FieldByName("_id")
		require.NotNil(t, idField)
		assert.Equal(t, "_id", idField.JSONName)
	})

	t.Run("tagged options", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[taggedDoc]())
		require.NoError(t, err)

		skuField := info.FieldByName("sku")
		require.NotNil(t, skuField)
		assert.True(t, skuField.Options.Unique)

		bodyField := info.FieldByName("body")
		require.NotNil(t, bodyField)
		assert.True(t, bodyField.Options.FTS)

		tagsField := info.FieldByName("tags")
		require.NotNil(t, tagsField)
		assert.True(t, tagsField.Options.Index)
	})

	t.Run("no json tag uses lowercase field name", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[noTagDoc]())
		require.NoError(t, err)

		titleField := info.FieldByName("title")
		require.NotNil(t, titleField)
		assert.Equal(t, "title", titleField.JSONName)

		countField := info.FieldByName("count")
		require.NotNil(t, countField)
		assert.Equal(t, "count", countField.JSONName)
	})

	t.Run("field without any tag uses lowercase go name", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[taggedDoc]())
		require.NoError(t, err)

		plainField := info.FieldByName("plain")
		require.NotNil(t, plainField)
		assert.Equal(t, "plain", plainField.JSONName)
	})

	t.Run("detects soft base", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[softDoc]())
		require.NoError(t, err)

		assert.True(t, info.HasDeletedAt)
		deletedField := info.FieldByName("_deleted_at")
		require.NotNil(t, deletedField)
	})

	t.Run("no soft base on regular doc", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[simpleDoc]())
		require.NoError(t, err)

		assert.False(t, info.HasDeletedAt)
	})

	t.Run("indexed fields", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[simpleDoc]())
		require.NoError(t, err)

		indexed := info.IndexedFields()
		assert.Len(t, indexed, 1)
		assert.Equal(t, "price", indexed[0].JSONName)
	})

	t.Run("unique fields", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[taggedDoc]())
		require.NoError(t, err)

		unique := info.UniqueFields()
		assert.Len(t, unique, 1)
		assert.Equal(t, "sku", unique[0].JSONName)
	})
}

func TestCollectionName(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		want     string
	}{
		{"simple", "Product", "product"},
		{"camel case", "UserProfile", "userprofile"},
		{"already lower", "note", "note"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CollectionName(tt.typeName))
		})
	}
}
