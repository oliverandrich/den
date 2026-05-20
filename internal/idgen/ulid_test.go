package idgen

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_FormatAndAlphabet(t *testing.T) {
	id := New()
	assert.Len(t, id, 26, "ULID must be 26 chars")
	for _, c := range id {
		assert.Contains(t, crockfordAlphabet, string(c),
			"all chars must be in Crockford base32 alphabet")
	}
}

func TestNew_KnownVectors(t *testing.T) {
	cases := []struct {
		name string
		ms   uint64
		rand [10]byte
		want string
	}{
		{
			name: "all zero",
			ms:   0,
			rand: [10]byte{},
			want: "00000000000000000000000000",
		},
		{
			name: "ms 0, rand last byte = 1",
			ms:   0,
			rand: [10]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			want: "00000000000000000000000001",
		},
		{
			name: "ms max 48 bit, rand all 0xFF",
			ms:   (1 << 48) - 1,
			rand: [10]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			want: "7ZZZZZZZZZZZZZZZZZZZZZZZZZ",
		},
		{
			name: "ms 1, all rand zero",
			ms:   1,
			rand: [10]byte{},
			want: "00000000010000000000000000",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := encode(tc.ms, tc.rand)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNew_Sortable(t *testing.T) {
	const n = 1000
	ids := make([]string, n)
	for i := range n {
		ids[i] = New()
	}
	for i := 1; i < n; i++ {
		assert.Less(t, ids[i-1], ids[i],
			"sequential IDs must be strictly ascending as strings (i=%d)", i)
	}
}

func TestNew_MonotonicWithinSameMs(t *testing.T) {
	g := newGenerator(func() uint64 { return 12345 })
	const n = 10000
	ids := make([]string, n)
	for i := range n {
		ids[i] = g.new()
	}
	for i := 1; i < n; i++ {
		require.Less(t, ids[i-1], ids[i],
			"intra-millisecond IDs must be strictly ascending (i=%d)", i)
	}
}

func TestNew_ParallelUnique(t *testing.T) {
	const goroutines = 16
	const perGoroutine = 5000

	all := make([]string, 0, goroutines*perGoroutine)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			local := make([]string, perGoroutine)
			for i := range perGoroutine {
				local[i] = New()
			}
			mu.Lock()
			all = append(all, local...)
			mu.Unlock()
		})
	}
	wg.Wait()

	seen := make(map[string]struct{}, len(all))
	for _, id := range all {
		_, dup := seen[id]
		require.False(t, dup, "duplicate ID across goroutines: %s", id)
		seen[id] = struct{}{}
	}
}

func TestNew_TimestampRecoverable(t *testing.T) {
	const ms uint64 = 1716000000000
	id := encode(ms, [10]byte{})
	got := decodeTimestamp(id)
	assert.Equal(t, ms, got)
}

func TestNew_BackwardClock_PreservesMonotonicity(t *testing.T) {
	ticks := []uint64{2000, 2000, 1500, 1500, 1500, 1000}
	idx := 0
	g := newGenerator(func() uint64 {
		v := ticks[idx]
		idx++
		return v
	})
	ids := make([]string, len(ticks))
	for i := range ticks {
		ids[i] = g.new()
	}
	for i := 1; i < len(ids); i++ {
		assert.Less(t, ids[i-1], ids[i],
			"clock rewind must not break monotonicity (i=%d, ticks=%v)", i, ticks)
	}
}

func TestNew_RandOverflow_RecoversByReseed(t *testing.T) {
	g := newGenerator(func() uint64 { return 7777 })
	for i := range 10 {
		g.lastRand[i] = 0xFF
	}
	g.lastMs = 7777

	assert.NotPanics(t, func() {
		_ = g.new()
	}, "overflow must re-seed, not panic")

	for i := 1; i < 100; i++ {
		_ = g.new()
	}
}

func TestNew_IntraMsStep_IsRandomized(t *testing.T) {
	g := newGenerator(func() uint64 { return 12345 })
	seed(&g.lastRand)
	prev := g.lastRand[9]
	nonOneDeltas := 0
	const n = 200
	for range n {
		_ = g.new()
		delta := g.lastRand[9] - prev
		if delta != 1 {
			nonOneDeltas++
		}
		prev = g.lastRand[9]
	}
	// With a 32-bit random step, the low byte of the increment is
	// uniform in [0, 255]. P(delta == 1) ≈ 1/256, so across 200
	// iterations we expect ~0.8 occurrences. A hard floor at >n/2 is
	// astronomically safe and catches accidental regressions to +1.
	require.Greater(t, nonOneDeltas, n/2,
		"intra-ms increment must be a random step, not +1")
}

func TestEncode_TimestampOrdering(t *testing.T) {
	a := encode(1000, [10]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	b := encode(1001, [10]byte{})
	assert.Less(t, a, b,
		"timestamp must dominate sort order over randomness")
}

func TestCrockfordAlphabet_NoConfusingChars(t *testing.T) {
	for _, ambiguous := range "ILOU" {
		assert.NotContains(t, crockfordAlphabet, string(ambiguous),
			"Crockford alphabet must not contain %c", ambiguous)
	}
	assert.Len(t, crockfordAlphabet, 32, "Crockford alphabet must be exactly 32 chars")
}

// decodeTimestamp extracts the 48-bit millisecond timestamp from the first
// 10 characters of a ULID. Test helper, not part of the package API.
func decodeTimestamp(id string) uint64 {
	var ms uint64
	for i := range 10 {
		ms = (ms << 5) | uint64(strings.IndexByte(crockfordAlphabet, id[i]))
	}
	return ms
}
