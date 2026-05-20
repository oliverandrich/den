package util

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

type compositeDoc struct {
	testBase
	UserID string `json:"user_id" den:"unique_together:user_name"`
	Name   string `json:"name" den:"unique_together:user_name"`
	FeedID string `json:"feed_id" den:"index_together:feed_date"`
	Date   string `json:"date" den:"index_together:feed_date"`
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
		wantErr  bool
	}{
		{"empty", "", TagOptions{}, false},
		{"index", "index", TagOptions{Index: true}, false},
		{"unique", "unique", TagOptions{Unique: true}, false},
		{"fts", "fts", TagOptions{FTS: true}, false},
		{"multiple", "index,fts", TagOptions{Index: true, FTS: true}, false},
		{"all", "index,unique,fts", TagOptions{Index: true, Unique: true, FTS: true}, false},
		{"unique_together", "unique_together:feed_guid", TagOptions{UniqueTogether: "feed_guid"}, false},
		{"index_together", "index_together:user_feed", TagOptions{IndexTogether: "user_feed"}, false},
		{"unique_together with index", "index,unique_together:composite", TagOptions{Index: true, UniqueTogether: "composite"}, false},
		{"index_together with fts", "fts,index_together:group1", TagOptions{FTS: true, IndexTogether: "group1"}, false},
		{"omitempty", "omitempty", TagOptions{OmitEmpty: true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := ParseDenTag(tt.tag)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantOpts, opts)
			}
		})
	}
}

func TestParseDenTag_UnknownOption(t *testing.T) {
	_, err := ParseDenTag("nonsense")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown den tag option")
	assert.Contains(t, err.Error(), "nonsense")
}

func TestParseDenTag_UnknownMixed(t *testing.T) {
	_, err := ParseDenTag("index,typo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "typo")
}

// The following types carry intentionally malformed json tags so we can
// verify AnalyzeStruct rejects them. staticcheck (SA5008) flags the tags
// as invalid JSON field names — that is exactly what we're testing.
//
//nolint:staticcheck
func TestAnalyzeStruct_RejectsInvalidJSONName(t *testing.T) {
	type badSemicolon struct {
		testBase
		X string `json:"name';DROP TABLE foo;--"`
	}
	type badQuote struct {
		testBase
		X string `json:"a'b"`
	}
	type badDoubleQuote struct {
		testBase
		X string `json:"a\"b"`
	}
	type badDot struct {
		testBase
		X string `json:"a.b"`
	}
	type badSpace struct {
		testBase
		X string `json:"a b"`
	}

	tests := []struct {
		name string
		typ  reflect.Type
	}{
		{"semicolon injection", reflect.TypeFor[badSemicolon]()},
		{"single quote", reflect.TypeFor[badQuote]()},
		{"double quote", reflect.TypeFor[badDoubleQuote]()},
		{"dot", reflect.TypeFor[badDot]()},
		{"space", reflect.TypeFor[badSpace]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := AnalyzeStruct(tt.typ)
			require.ErrorIs(t, err, ErrInvalidFieldName)
			assert.Contains(t, err.Error(), "field X", "error should name the Go field")
		})
	}
}

func TestAnalyzeStruct_NoJSONTagFallsBackToLowercase(t *testing.T) {
	_, err := AnalyzeStruct(reflect.TypeFor[noTagDoc]())
	require.NoError(t, err, "empty json tag should fall back to strings.ToLower(fieldname), which is always valid")
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

	t.Run("unique_together fields", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[compositeDoc]())
		require.NoError(t, err)

		userIDField := info.FieldByName("user_id")
		require.NotNil(t, userIDField)
		assert.Equal(t, "user_name", userIDField.Options.UniqueTogether)

		nameField := info.FieldByName("name")
		require.NotNil(t, nameField)
		assert.Equal(t, "user_name", nameField.Options.UniqueTogether)
	})

	t.Run("index_together fields", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[compositeDoc]())
		require.NoError(t, err)

		feedIDField := info.FieldByName("feed_id")
		require.NotNil(t, feedIDField)
		assert.Equal(t, "feed_date", feedIDField.Options.IndexTogether)

		dateField := info.FieldByName("date")
		require.NotNil(t, dateField)
		assert.Equal(t, "feed_date", dateField.Options.IndexTogether)
	})
}

type validateTaggedDoc struct {
	testBase
	Name string `json:"name" validate:"required,min=3"`
}

type embeddedValidateInner struct {
	Email string `json:"email" validate:"required,email"`
}

type validateTaggedEmbeddedDoc struct {
	testBase
	embeddedValidateInner
	Title string `json:"title"`
}

type validateDashTaggedDoc struct {
	testBase
	Ignored string `json:"ignored" validate:"-"`
}

type validateInnerNamedStruct struct {
	StoragePath string `json:"storage_path" validate:"required,max=1024"`
	Mime        string `json:"mime"         validate:"required,max=100"`
}

type validateTaggedNamedStructDoc struct {
	testBase
	Hero validateInnerNamedStruct `json:"hero"`
}

type validateTaggedPointerStructDoc struct {
	testBase
	Hero *validateInnerNamedStruct `json:"hero,omitempty"`
}

type validateTaggedSliceStructDoc struct {
	testBase
	Items []validateInnerNamedStruct `json:"items"`
}

type plainNamedStruct struct {
	StoragePath string `json:"storage_path"`
	Mime        string `json:"mime"`
}

type plainNamedStructDoc struct {
	testBase
	Hero plainNamedStruct `json:"hero"`
}

func TestAnalyzeStruct_HasValidateTags(t *testing.T) {
	t.Run("no validate tags", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[noTagDoc]())
		require.NoError(t, err)
		assert.False(t, info.HasValidateTags)
	})

	t.Run("no validate tags only den tags", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[taggedDoc]())
		require.NoError(t, err)
		assert.False(t, info.HasValidateTags)
	})

	t.Run("validate tag on top-level field", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[validateTaggedDoc]())
		require.NoError(t, err)
		assert.True(t, info.HasValidateTags)
	})

	t.Run("validate tag on embedded struct field", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[validateTaggedEmbeddedDoc]())
		require.NoError(t, err)
		assert.True(t, info.HasValidateTags,
			"validate: tags on fields of an anonymous embedded struct must be detected")
	})

	t.Run("validate dash is treated as no tag", func(t *testing.T) {
		// go-playground/validator treats validate:"-" as an explicit skip
		// for the field and its children — no constraints fire. Mirror
		// that: a doc whose only validate: tag is "-" should short-circuit
		// like a tagless type and avoid the walker cost.
		info, err := AnalyzeStruct(reflect.TypeFor[validateDashTaggedDoc]())
		require.NoError(t, err)
		assert.False(t, info.HasValidateTags)
	})

	t.Run("validate dash on parent struct blocks descent", func(t *testing.T) {
		// A `validate:"-"` on a parent field stops the validator from
		// recursing into the nested type. The scanner must mirror that or
		// it would flag types as needing validation that the validator
		// itself would skip.
		type outer struct {
			testBase
			Inner validateInnerNamedStruct `json:"inner" validate:"-"`
		}
		info, err := AnalyzeStruct(reflect.TypeFor[outer]())
		require.NoError(t, err)
		assert.False(t, info.HasValidateTags)
	})

	t.Run("validate dash mixed with real tag still flags", func(t *testing.T) {
		type mixed struct {
			testBase
			Skipped string `json:"skipped" validate:"-"`
			Name    string `json:"name"    validate:"required"`
		}
		info, err := AnalyzeStruct(reflect.TypeFor[mixed]())
		require.NoError(t, err)
		assert.True(t, info.HasValidateTags)
	})

	t.Run("validate tag inside a named struct field", func(t *testing.T) {
		// go-playground/validator descends into named (non-anonymous) struct
		// fields by default, so a doc whose only validate: tags live inside
		// a named struct field (e.g. Hero document.Attachment) must still
		// trigger the walk. Skipping it would silently disable validation.
		info, err := AnalyzeStruct(reflect.TypeFor[validateTaggedNamedStructDoc]())
		require.NoError(t, err)
		assert.True(t, info.HasValidateTags)
	})

	t.Run("validate tag inside a pointer-to-struct field", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[validateTaggedPointerStructDoc]())
		require.NoError(t, err)
		assert.True(t, info.HasValidateTags)
	})

	t.Run("validate tag inside a slice element type", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[validateTaggedSliceStructDoc]())
		require.NoError(t, err)
		assert.True(t, info.HasValidateTags)
	})

	t.Run("nested struct field without validate tags", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[plainNamedStructDoc]())
		require.NoError(t, err)
		assert.False(t, info.HasValidateTags)
	})
}

// Types for nested-field walker tests (den-1351). The walker must descend
// into named struct fields and pointer-to-struct fields, producing dotted
// JSONName paths so `den:` tags inside them become real schema constraints.

type nestedInner struct {
	Slug       string `json:"slug"       den:"unique"`
	Department string `json:"department" den:"index"`
	Bio        string `json:"bio"`
}

type nestedValueDoc struct {
	testBase
	Profile nestedInner `json:"profile"`
}

type nestedPointerDoc struct {
	testBase
	Profile *nestedInner `json:"profile,omitempty"`
}

type nestedDepth3Inner struct {
	Zip string `json:"zip" den:"index"`
}

type nestedDepth2Inner struct {
	City nestedDepth3Inner `json:"city"`
}

type nestedDepth3Doc struct {
	testBase
	Addr nestedDepth2Inner `json:"addr"`
}

type nestedCycleDoc struct {
	testBase
	Name  string          `json:"name" den:"index"`
	Child *nestedCycleDoc `json:"child,omitempty"`
}

type nestedFTSInner struct {
	Bio string `json:"bio" den:"fts"`
}

type nestedFTSDoc struct {
	testBase
	Profile nestedFTSInner `json:"profile"`
}

type nestedBadSegmentInner struct {
	Bad string `json:"bad name"` //nolint:staticcheck // intentionally invalid
}

type nestedBadSegmentDoc struct {
	testBase
	Profile nestedBadSegmentInner `json:"profile"`
}

type nestedTimeFieldDoc struct {
	testBase
	CreatedAt  time.Time  `json:"created_at"  den:"index"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty" den:"index"`
	HiddenName string     `json:"hidden_name"`
}

// linkShape is structurally a Link[T] from internal/core (ID/Value/Loaded
// with string ID). The walker must NOT recurse into it.
type linkShape struct {
	ID     string
	Value  *int
	Loaded bool
}

type docWithLinkLikeField struct {
	testBase
	Ref linkShape `json:"ref"`
}

type docWithLinkLikeSlice struct {
	testBase
	Refs []linkShape `json:"refs"`
}

type nestedUTInner struct {
	Slug string `json:"slug" den:"unique_together:user_slug"`
}

type nestedUTSpanDoc struct {
	testBase
	UserID  string        `json:"user_id" den:"unique_together:user_slug"`
	Profile nestedUTInner `json:"profile"`
}

func TestIsLinkShape(t *testing.T) {
	type matchesShape struct {
		ID     string
		Value  *int
		Loaded bool
	}
	type missingValue struct {
		ID     string
		Loaded bool
	}
	type missingLoaded struct {
		ID    string
		Value *int
	}
	type idIsNotString struct {
		ID     int
		Value  *int
		Loaded bool
	}
	type plain struct {
		Foo string
	}

	tests := []struct {
		name string
		typ  reflect.Type
		want bool
	}{
		{"link shape", reflect.TypeFor[matchesShape](), true},
		{"missing Value", reflect.TypeFor[missingValue](), false},
		{"missing Loaded", reflect.TypeFor[missingLoaded](), false},
		{"ID not string", reflect.TypeFor[idIsNotString](), false},
		{"plain struct", reflect.TypeFor[plain](), false},
		{"non-struct", reflect.TypeFor[string](), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsLinkShape(tt.typ))
		})
	}
}

func TestAnalyzeStruct_RecursesNamedStructField(t *testing.T) {
	info, err := AnalyzeStruct(reflect.TypeFor[nestedValueDoc]())
	require.NoError(t, err)

	slug := info.FieldByName("profile.slug")
	require.NotNil(t, slug, "nested unique field must show up as profile.slug")
	assert.True(t, slug.Options.Unique)

	dept := info.FieldByName("profile.department")
	require.NotNil(t, dept)
	assert.True(t, dept.Options.Index)

	// Untagged inner field must also be captured so SetFields lookups work.
	bio := info.FieldByName("profile.bio")
	require.NotNil(t, bio)
	assert.False(t, bio.Options.Index)
}

func TestAnalyzeStruct_RecursesPointerToStructField(t *testing.T) {
	info, err := AnalyzeStruct(reflect.TypeFor[nestedPointerDoc]())
	require.NoError(t, err)

	slug := info.FieldByName("profile.slug")
	require.NotNil(t, slug, "nested unique field on pointer-to-struct must be reachable")
	assert.True(t, slug.Options.Unique)
}

func TestAnalyzeStruct_NestedDepthThree(t *testing.T) {
	info, err := AnalyzeStruct(reflect.TypeFor[nestedDepth3Doc]())
	require.NoError(t, err)

	zip := info.FieldByName("addr.city.zip")
	require.NotNil(t, zip, "depth-3 nested field must be reachable via dotted path")
	assert.True(t, zip.Options.Index)
}

func TestAnalyzeStruct_TimeIsLeafScalar(t *testing.T) {
	info, err := AnalyzeStruct(reflect.TypeFor[nestedTimeFieldDoc]())
	require.NoError(t, err)

	createdAt := info.FieldByName("created_at")
	require.NotNil(t, createdAt)
	assert.True(t, createdAt.Options.Index)

	updatedAt := info.FieldByName("updated_at")
	require.NotNil(t, updatedAt)
	assert.True(t, updatedAt.Options.Index)

	for _, f := range info.Fields {
		assert.NotContains(t, f.JSONName, "created_at.",
			"walker must not recurse into time.Time — would produce stdlib internals")
		assert.NotContains(t, f.JSONName, "updated_at.",
			"walker must not recurse into *time.Time")
	}
}

func TestAnalyzeStruct_SkipsLinkShapedFields(t *testing.T) {
	t.Run("named link field", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[docWithLinkLikeField]())
		require.NoError(t, err)

		// The Ref field itself is captured as a leaf — but the walker
		// must NOT descend into ID/Value/Loaded.
		ref := info.FieldByName("ref")
		require.NotNil(t, ref)
		for _, f := range info.Fields {
			assert.NotContains(t, f.JSONName, "ref.",
				"walker must not recurse into Link-shaped structs")
		}
	})

	t.Run("slice of link field", func(t *testing.T) {
		info, err := AnalyzeStruct(reflect.TypeFor[docWithLinkLikeSlice]())
		require.NoError(t, err)

		refs := info.FieldByName("refs")
		require.NotNil(t, refs)
		for _, f := range info.Fields {
			assert.NotContains(t, f.JSONName, "refs.")
		}
	})
}

func TestAnalyzeStruct_SiblingsOfSameType(t *testing.T) {
	// Pins the stack-style cycle protection: seen[t] is set on entry
	// and deleted on exit, so two sibling fields of the same struct
	// type are both walked. A "permanent mark" implementation would
	// silently drop the second one and still satisfy the existing
	// cycle test — this case catches that refactor.
	type sibInner struct {
		X string `json:"x" den:"index"`
	}
	type sibDoc struct {
		testBase
		A sibInner `json:"a"`
		B sibInner `json:"b"`
	}

	info, err := AnalyzeStruct(reflect.TypeFor[sibDoc]())
	require.NoError(t, err)

	require.NotNil(t, info.FieldByName("a.x"), "first sibling must be walked")
	require.NotNil(t, info.FieldByName("b.x"), "second sibling of same type must also be walked")
}

func TestAnalyzeStruct_CycleProtection(t *testing.T) {
	info, err := AnalyzeStruct(reflect.TypeFor[nestedCycleDoc]())
	require.NoError(t, err, "self-referential pointer must not cause infinite recursion")

	// Top-level fields are present.
	require.NotNil(t, info.FieldByName("name"))
	require.NotNil(t, info.FieldByName("child"),
		"self-referential pointer is captured as a leaf field")
	// The walker refuses to enter the same type a second time, so no
	// nested fields beneath child are recorded — schema would otherwise
	// be unbounded.
	assert.Nil(t, info.FieldByName("child.name"),
		"cycle protection blocks recursion when the type is already on the stack")
}

func TestAnalyzeStruct_AcceptsFTSOnNestedField(t *testing.T) {
	info, err := AnalyzeStruct(reflect.TypeFor[nestedFTSDoc]())
	require.NoError(t, err, "den:\"fts\" on a nested field must be honoured after den-ciug")

	bio := info.FieldByName("profile.bio")
	require.NotNil(t, bio)
	assert.True(t, bio.Options.FTS, "nested FTS tag must flow through to the field metadata")
}

func TestAnalyzeStruct_NestedSegmentValidation(t *testing.T) {
	_, err := AnalyzeStruct(reflect.TypeFor[nestedBadSegmentDoc]())
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidFieldName,
		"invalid json tag on a nested field must surface as ErrInvalidFieldName")
}

func TestAnalyzeStruct_UniqueTogetherSpansLevels(t *testing.T) {
	info, err := AnalyzeStruct(reflect.TypeFor[nestedUTSpanDoc]())
	require.NoError(t, err)

	userID := info.FieldByName("user_id")
	require.NotNil(t, userID)
	assert.Equal(t, "user_slug", userID.Options.UniqueTogether)

	slug := info.FieldByName("profile.slug")
	require.NotNil(t, slug)
	assert.Equal(t, "user_slug", slug.Options.UniqueTogether,
		"unique_together group must be honoured on nested fields")
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
