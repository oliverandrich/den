package engine_test

import (
	"github.com/oliverandrich/den/engine"

	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewID_Length(t *testing.T) {
	id := engine.NewID()
	assert.Len(t, id, 26, "ULID is a 26-char Crockford-base32 string")
}

func TestNewID_Unique(t *testing.T) {
	a := engine.NewID()
	b := engine.NewID()
	assert.NotEqual(t, a, b, "back-to-back NewID calls must not collide")
}

func TestNewID_BulkUnique(t *testing.T) {
	const N = 10000
	seen := make(map[string]struct{}, N)
	for range N {
		id := engine.NewID()
		_, dup := seen[id]
		require.False(t, dup, "duplicate id %q at sample size %d", id, len(seen))
		seen[id] = struct{}{}
	}
	assert.Len(t, seen, N)
}

func TestNewID_TimeSortable(t *testing.T) {
	id1 := engine.NewID()
	// ULIDs are only time-ordered at millisecond granularity — sleep past a
	// millisecond boundary so the second ID is guaranteed to have a later
	// timestamp prefix.
	time.Sleep(2 * time.Millisecond)
	id2 := engine.NewID()

	assert.Less(t, id1, id2,
		"IDs from different milliseconds must be lexicographically ordered")
}
