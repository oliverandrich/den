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

func TestTracked_ZeroValue(t *testing.T) {
	var tr Tracked
	assert.Nil(t, tr.Snapshot())
}

func TestTracked_SetSnapshot(t *testing.T) {
	var tr Tracked
	data := []byte(`{"name":"test"}`)
	tr.SetSnapshot(data)
	assert.Equal(t, data, tr.Snapshot())
}

func TestSoftDelete_ZeroValue(t *testing.T) {
	var s SoftDelete
	assert.False(t, s.IsDeleted())
}

func TestSoftDelete_AfterSetting(t *testing.T) {
	now := time.Now()
	s := SoftDelete{DeletedAt: &now}
	assert.True(t, s.IsDeleted())
}

// TestComposition confirms the three embeds work side-by-side on a single
// struct — this is the whole point of splitting them out from the old
// old TrackedSoftBase monolith was a 2² matrix; now it's free composition.
func TestComposition_BaseSoftDeleteTracked(t *testing.T) {
	type AuditLog struct {
		Base
		SoftDelete
		Tracked
		Action string
	}

	var a AuditLog
	a.ID = "log-1"
	a.Action = "create"

	// All three embeds' methods promoted without collision.
	assert.False(t, a.IsDeleted())
	assert.Nil(t, a.Snapshot())

	a.SetSnapshot([]byte(`{"action":"create"}`))
	assert.NotNil(t, a.Snapshot())

	now := time.Now()
	a.DeletedAt = &now
	assert.True(t, a.IsDeleted())
}
