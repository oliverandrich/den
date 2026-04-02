package document

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewID(t *testing.T) {
	id1 := NewID()
	id2 := NewID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Len(t, id1, 26) // ULID is 26 chars
}

func TestNewID_Sortable(t *testing.T) {
	id1 := NewID()
	time.Sleep(2 * time.Millisecond)
	id2 := NewID()

	// ULIDs generated in different milliseconds are lexicographically ordered
	assert.Less(t, id1, id2, "ULIDs should be lexicographically sortable")
}

func TestTrackedBase_ZeroValue(t *testing.T) {
	var tb TrackedBase
	assert.Nil(t, tb.Snapshot())
}

func TestTrackedBase_SetSnapshot(t *testing.T) {
	var tb TrackedBase
	data := []byte(`{"name":"test"}`)
	tb.SetSnapshot(data)
	assert.Equal(t, data, tb.Snapshot())
}

func TestTrackedSoftBase_ZeroValue(t *testing.T) {
	var tsb TrackedSoftBase
	assert.Nil(t, tsb.Snapshot())
}

func TestTrackedSoftBase_SetSnapshot(t *testing.T) {
	var tsb TrackedSoftBase
	data := []byte(`{"name":"test"}`)
	tsb.SetSnapshot(data)
	assert.Equal(t, data, tsb.Snapshot())
}

func TestTrackedSoftBase_IsDeleted(t *testing.T) {
	var tsb TrackedSoftBase
	assert.False(t, tsb.IsDeleted(), "zero-value should not be deleted")

	now := time.Now()
	tsb.DeletedAt = &now
	assert.True(t, tsb.IsDeleted(), "should be deleted after setting DeletedAt")
}
