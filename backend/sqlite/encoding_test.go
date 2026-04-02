package sqlite

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testBase struct {
	ID        string    `json:"_id"`
	CreatedAt time.Time `json:"_created_at"`
	UpdatedAt time.Time `json:"_updated_at"`
}

type testProduct struct {
	testBase
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

type testOmitEmpty struct {
	testBase
	Name  string `json:"name"`
	Notes string `json:"notes,omitempty"`
}

func TestJSONEncoder_EncodeDecode(t *testing.T) {
	enc := newJSONEncoder()

	p := &testProduct{
		testBase: testBase{ID: "p1"},
		Name:     "Widget",
		Price:    29.99,
	}

	data, err := enc.Encode(p)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"name":"Widget"`)

	decoded := &testProduct{}
	err = enc.Decode(data, decoded)
	require.NoError(t, err)
	assert.Equal(t, "Widget", decoded.Name)
	assert.InDelta(t, 29.99, decoded.Price, 0.001)
	assert.Equal(t, "p1", decoded.ID)
}

func TestJSONEncoder_OmitEmpty(t *testing.T) {
	enc := newJSONEncoder()

	p := &testOmitEmpty{
		testBase: testBase{ID: "p1"},
		Name:     "Widget",
		Notes:    "",
	}

	data, err := enc.Encode(p)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "notes")
}

func TestJSONEncoder_OmitEmpty_NonZero(t *testing.T) {
	enc := newJSONEncoder()

	p := &testOmitEmpty{
		testBase: testBase{ID: "p1"},
		Name:     "Widget",
		Notes:    "hello",
	}

	data, err := enc.Encode(p)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"notes":"hello"`)
}

func TestJSONEncoder_DecodeToMap(t *testing.T) {
	enc := newJSONEncoder()

	data := []byte(`{"_id":"p1","name":"Widget","price":10}`)
	var m map[string]any
	err := enc.Decode(data, &m)
	require.NoError(t, err)
	assert.Equal(t, "p1", m["_id"])
	assert.Equal(t, "Widget", m["name"])
}

func TestJSONEncoder_IntField(t *testing.T) {
	type doc struct {
		testBase
		Count int `json:"count"`
	}

	enc := newJSONEncoder()
	d := &doc{testBase: testBase{ID: "d1"}, Count: 42}

	data, err := enc.Encode(d)
	require.NoError(t, err)

	decoded := &doc{}
	require.NoError(t, enc.Decode(data, decoded))
	assert.Equal(t, 42, decoded.Count)
}

func TestJSONEncoder_SliceField(t *testing.T) {
	type doc struct {
		testBase
		Tags []string `json:"tags"`
	}

	enc := newJSONEncoder()
	d := &doc{testBase: testBase{ID: "d1"}, Tags: []string{"a", "b"}}

	data, err := enc.Encode(d)
	require.NoError(t, err)

	decoded := &doc{}
	require.NoError(t, enc.Decode(data, decoded))
	assert.Equal(t, []string{"a", "b"}, decoded.Tags)
}

func TestJSONEncoder_PointerField(t *testing.T) {
	type doc struct {
		testBase
		Email *string `json:"email"`
	}

	enc := newJSONEncoder()
	email := "test@example.com"
	d := &doc{testBase: testBase{ID: "d1"}, Email: &email}

	data, err := enc.Encode(d)
	require.NoError(t, err)

	decoded := &doc{}
	require.NoError(t, enc.Decode(data, decoded))
	require.NotNil(t, decoded.Email)
	assert.Equal(t, "test@example.com", *decoded.Email)
}

func TestJSONEncoder_UintField(t *testing.T) {
	type doc struct {
		testBase
		Count uint `json:"count"`
	}

	enc := newJSONEncoder()
	d := &doc{testBase: testBase{ID: "d1"}, Count: 42}

	data, err := enc.Encode(d)
	require.NoError(t, err)

	decoded := &doc{}
	require.NoError(t, enc.Decode(data, decoded))
	assert.Equal(t, uint(42), decoded.Count)
}

func TestJSONEncoder_NilPointerField(t *testing.T) {
	type doc struct {
		testBase
		Email *string `json:"email"`
	}

	enc := newJSONEncoder()
	d := &doc{testBase: testBase{ID: "d1"}, Email: nil}

	data, err := enc.Encode(d)
	require.NoError(t, err)

	decoded := &doc{}
	require.NoError(t, enc.Decode(data, decoded))
	assert.Nil(t, decoded.Email)
}
