package document

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSoftBase_IsDeleted(t *testing.T) {
	t.Run("not deleted", func(t *testing.T) {
		s := SoftBase{}
		assert.False(t, s.IsDeleted())
	})

	t.Run("deleted", func(t *testing.T) {
		now := time.Now()
		s := SoftBase{DeletedAt: &now}
		assert.True(t, s.IsDeleted())
	})
}
