package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testBase struct {
	ID string `json:"_id"`
}

type testProduct struct {
	testBase
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

func TestJSONEncoder_RoundTrip(t *testing.T) {
	enc := newJSONEncoder()

	p := &testProduct{testBase: testBase{ID: "p1"}, Name: "Widget", Price: 29.99}
	data, err := enc.Encode(p)
	require.NoError(t, err)

	decoded := &testProduct{}
	require.NoError(t, enc.Decode(data, decoded))
	assert.Equal(t, "Widget", decoded.Name)
	assert.InDelta(t, 29.99, decoded.Price, 0.001)
	assert.Equal(t, "p1", decoded.ID)
}

func TestJSONEncoder_DecodeToMap(t *testing.T) {
	enc := newJSONEncoder()
	data := []byte(`{"_id":"p1","name":"Widget"}`)

	var m map[string]any
	require.NoError(t, enc.Decode(data, &m))
	assert.Equal(t, "p1", m["_id"])
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

func TestJSONEncoder_NilPointer(t *testing.T) {
	type doc struct {
		testBase
		Email *string `json:"email"`
	}
	enc := newJSONEncoder()
	d := &doc{testBase: testBase{ID: "d1"}}
	data, err := enc.Encode(d)
	require.NoError(t, err)
	decoded := &doc{}
	require.NoError(t, enc.Decode(data, decoded))
	assert.Nil(t, decoded.Email)
}

func TestJSONEncoder_SkipTag(t *testing.T) {
	type doc struct {
		testBase
		Internal string `json:"-"`
		Name     string `json:"name"`
	}
	enc := newJSONEncoder()
	d := &doc{testBase: testBase{ID: "d1"}, Internal: "secret", Name: "visible"}
	data, err := enc.Encode(d)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "secret")
	assert.Contains(t, string(data), "visible")
}

func TestJSONEncoder_OmitEmpty(t *testing.T) {
	type doc struct {
		testBase
		Notes string `json:"notes,omitempty"`
	}

	enc := newJSONEncoder()
	d := &doc{testBase: testBase{ID: "d1"}, Notes: ""}
	data, err := enc.Encode(d)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "notes")
}
