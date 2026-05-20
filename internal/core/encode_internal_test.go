package core

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// (*DB).encode is state-free — it reads no fields from db, so a zero
// *DB suffices for direct tests of the encoding contract.

func TestDBEncode_NoHTMLEscape(t *testing.T) {
	db := &DB{}
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"ampersand", map[string]string{"k": "a & b"}, `{"k":"a & b"}`},
		{"angles", map[string]string{"k": "x<y>z"}, `{"k":"x<y>z"}`},
		{"markup_with_url", map[string]string{"k": `<a href="x?a=1&b=2">link</a>`}, `{"k":"<a href=\"x?a=1&b=2\">link</a>"}`},
		{"plain", map[string]string{"k": "plain"}, `{"k":"plain"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := db.encode(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

func TestDBEncode_NoTrailingNewline(t *testing.T) {
	db := &DB{}
	got, err := db.encode(map[string]int{"x": 1})
	require.NoError(t, err)
	require.NotEmpty(t, got)
	assert.False(t, strings.HasSuffix(string(got), "\n"),
		"trailing newline from Encoder.Encode must be stripped")
}

func TestDBEncode_RoundTrip(t *testing.T) {
	db := &DB{}
	in := map[string]string{"k": "<a&b>"}
	encoded, err := db.encode(in)
	require.NoError(t, err)

	var out map[string]string
	require.NoError(t, db.decode(encoded, &out))
	assert.Equal(t, in, out)
}

func TestDBDecode_AcceptsLegacyHTMLEscaped(t *testing.T) {
	db := &DB{}
	// Pre-change rows on disk carry the \uXXXX form. stdlib
	// json.Unmarshal accepts both forms, so backward compat is free.
	legacy := []byte(`{"k":"<a&b>"}`)
	var out map[string]string
	require.NoError(t, db.decode(legacy, &out))
	assert.Equal(t, map[string]string{"k": "<a&b>"}, out)
}

func TestDBEncode_PoolSafety(t *testing.T) {
	// Pins bytes.Clone: without it, the second encode would corrupt
	// the first returned slice when the pool recycles the buffer.
	db := &DB{}
	a, err := db.encode(map[string]string{"a": "first"})
	require.NoError(t, err)
	b, err := db.encode(map[string]string{"b": "second"})
	require.NoError(t, err)
	// bytes.Equal for byte-exact comparison (assert.JSONEq would be
	// semantic, which is the wrong contract here).
	assert.True(t, bytes.Equal([]byte(`{"a":"first"}`), a), "first slice corrupted: %s", a)
	assert.True(t, bytes.Equal([]byte(`{"b":"second"}`), b), "second slice corrupted: %s", b)
}

func TestDBEncode_OversizeBufferNotPooled(t *testing.T) {
	// A single large doc must not pin its multi-KB buffer in the
	// pool for the rest of the process. The Put path drops buffers
	// whose capacity exceeds encodeBufPoolMaxCap.
	db := &DB{}

	// Synthesise a doc whose encoded form exceeds the threshold.
	big := strings.Repeat("x", encodeBufPoolMaxCap+1024)
	_, err := db.encode(map[string]string{"big": big})
	require.NoError(t, err)

	// Subsequent Get must return a fresh (small) buffer, not the
	// large one we just produced.
	buf, ok := encodeBufPool.Get().(*bytes.Buffer)
	require.True(t, ok)
	assert.LessOrEqual(t, buf.Cap(), encodeBufPoolMaxCap,
		"pool retained oversize buffer (cap=%d)", buf.Cap())
}
