package engine_test

import (
	"github.com/oliverandrich/den/engine"

	"encoding/json"
	"testing"

	"github.com/oliverandrich/den/document"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// box is a self-referential link fixture for nested-expansion tests.
type box struct {
	document.Base
	Label string           `json:"label"`
	Inner engine.Link[box] `json:"inner"`
}

// tagged exercises json:"-" and omitempty alongside a link field.
type tagged struct {
	document.Base
	Secret string            `json:"-"`
	Note   string            `json:"note,omitempty"`
	Rel    engine.Link[Door] `json:"rel"`
}

// page is an envelope (non-document) wrapping link-bearing docs.
type page struct {
	Items []*House `json:"items"`
	Total int      `json:"total"`
}

func loadedDoor() *Door {
	d := &Door{Height: 200, Width: 80}
	d.ID = "door7"
	return d
}

func TestMarshal_RegressionUnloaded(t *testing.T) {
	h := &House{Name: "Casa"}
	h.ID = "house1"
	h.Door = engine.Link[Door]{ID: "door7"}
	h.Windows = []engine.Link[Window]{{ID: "w1"}, {ID: "w2"}}

	got, err := engine.Marshal(h)
	require.NoError(t, err)
	want, err := json.Marshal(h)
	require.NoError(t, err)
	assert.Equal(t, want, got, "byte-identical to json.Marshal when nothing is loaded")
}

func TestMarshal_LoadedSingleLink(t *testing.T) {
	h := &House{Name: "Casa"}
	h.ID = "house1"
	h.Door = engine.Link[Door]{ID: "door7", Value: loadedDoor(), Loaded: true}

	got, err := engine.Marshal(h)
	require.NoError(t, err)

	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &m))
	var door map[string]any
	require.NoError(t, json.Unmarshal(m["door"], &door))
	assert.Equal(t, "door7", door["_id"])
	assert.InDelta(t, 200.0, door["height"], 0)
}

func TestMarshal_UnloadedSingleLinkStaysID(t *testing.T) {
	h := &House{}
	h.ID = "h"
	h.Door = engine.Link[Door]{ID: "door7"}

	got, err := engine.Marshal(h)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &m))
	assert.JSONEq(t, `"door7"`, string(m["door"]))
}

func TestMarshal_SliceMixed(t *testing.T) {
	w1 := &Window{X: 1, Y: 2}
	w1.ID = "w1"
	h := &House{}
	h.ID = "h"
	h.Windows = []engine.Link[Window]{{ID: "w1", Value: w1, Loaded: true}, {ID: "w2"}}

	got, err := engine.Marshal(h)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &m))
	var arr []json.RawMessage
	require.NoError(t, json.Unmarshal(m["windows"], &arr))
	require.Len(t, arr, 2)
	var o map[string]any
	require.NoError(t, json.Unmarshal(arr[0], &o))
	assert.Equal(t, "w1", o["_id"])
	assert.JSONEq(t, `"w2"`, string(arr[1]))
}

func TestMarshal_NestedRecursion(t *testing.T) {
	inner := &box{Label: "inner"}
	inner.ID = "b2"
	inner.Inner = engine.Link[box]{ID: "b3"} // unloaded
	outer := &box{Label: "outer"}
	outer.ID = "b1"
	outer.Inner = engine.Link[box]{ID: "b2", Value: inner, Loaded: true}

	got, err := engine.Marshal(outer)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &m))
	var innerObj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(m["inner"], &innerObj))
	assert.JSONEq(t, `"inner"`, string(innerObj["label"]))
	assert.JSONEq(t, `"b3"`, string(innerObj["inner"]), "inner.inner stays id (unloaded)")
}

func TestMarshal_OmitemptyAndDashPreserved(t *testing.T) {
	tg := &tagged{Secret: "s"}
	tg.ID = "t1"
	tg.Rel = engine.Link[Door]{ID: "door7", Value: loadedDoor(), Loaded: true}

	got, err := engine.Marshal(tg)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &m))
	_, hasSecret := m["secret"]
	assert.False(t, hasSecret)
	_, hasSecretGo := m["Secret"]
	assert.False(t, hasSecretGo, "json:\"-\" field omitted")
	_, hasNote := m["note"]
	assert.False(t, hasNote, "omitempty empty value omitted")
	var rel map[string]any
	require.NoError(t, json.Unmarshal(m["rel"], &rel))
	assert.Equal(t, "door7", rel["_id"])
	_, hasID := m["_id"]
	assert.True(t, hasID, "embedded document.Base fields preserved")
}

func TestMarshal_PassThrough(t *testing.T) {
	b, err := engine.Marshal(nil)
	require.NoError(t, err)
	assert.JSONEq(t, "null", string(b))

	b, err = engine.Marshal(42)
	require.NoError(t, err)
	assert.Equal(t, "42", string(b))

	b, err = engine.Marshal("x")
	require.NoError(t, err)
	assert.JSONEq(t, `"x"`, string(b))

	// A struct with no link fields marshals exactly like json.Marshal.
	d := &Door{Height: 1}
	d.ID = "d"
	got, err := engine.Marshal(d)
	require.NoError(t, err)
	want, err := json.Marshal(d)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestMarshal_PointerAndSlices(t *testing.T) {
	h := House{Name: "Casa"}
	h.ID = "h"
	h.Door = engine.Link[Door]{ID: "door7", Value: loadedDoor(), Loaded: true}

	byVal, err := engine.Marshal(h)
	require.NoError(t, err)
	byPtr, err := engine.Marshal(&h)
	require.NoError(t, err)
	assert.Equal(t, byVal, byPtr)

	got, err := engine.Marshal([]*House{&h})
	require.NoError(t, err)
	var arr []json.RawMessage
	require.NoError(t, json.Unmarshal(got, &arr))
	require.Len(t, arr, 1)
}

// LinkEmbed + embedWrapper exercise link expansion through an anonymous
// embedded (exported) struct — its "d" key is promoted to the wrapper's top
// level.
type LinkEmbed struct {
	document.Base
	D engine.Link[Door] `json:"d"`
}

type embedWrapper struct {
	LinkEmbed
	Name string `json:"name"`
}

func TestMarshal_MapValues(t *testing.T) {
	h := &House{Name: "Casa"}
	h.ID = "h"
	h.Door = engine.Link[Door]{ID: "door7", Value: loadedDoor(), Loaded: true}

	got, err := engine.Marshal(map[string]*House{"a": h})
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &m))
	var item map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(m["a"], &item))
	var door map[string]any
	require.NoError(t, json.Unmarshal(item["door"], &door))
	assert.Equal(t, "door7", door["_id"], "loaded link inside a map value expands")
}

func TestMarshal_AnonymousEmbedWithLink(t *testing.T) {
	w := &embedWrapper{Name: "w"}
	w.ID = "e1"
	w.D = engine.Link[Door]{ID: "door7", Value: loadedDoor(), Loaded: true}

	got, err := engine.Marshal(w)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &m))
	var door map[string]any
	require.NoError(t, json.Unmarshal(m["d"], &door))
	assert.Equal(t, "door7", door["_id"], "link promoted from an anonymous embed expands")
}

func TestMarshal_EnvelopeExpands(t *testing.T) {
	h := &House{Name: "Casa"}
	h.ID = "h"
	h.Door = engine.Link[Door]{ID: "door7", Value: loadedDoor(), Loaded: true}
	p := page{Items: []*House{h}, Total: 1}

	got, err := engine.Marshal(p)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &m))
	assert.JSONEq(t, "1", string(m["total"]))
	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(m["items"], &items))
	require.Len(t, items, 1)
	var item map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(items[0], &item))
	var door map[string]any
	require.NoError(t, json.Unmarshal(item["door"], &door))
	assert.Equal(t, "door7", door["_id"], "loaded link expands even nested inside an envelope")
}
