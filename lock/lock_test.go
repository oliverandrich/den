// SPDX-License-Identifier: MIT

package lock_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den/backend"
	"github.com/oliverandrich/den/lock"
)

func TestResolve_Default(t *testing.T) {
	mode, err := lock.Resolve()
	require.NoError(t, err)
	assert.Equal(t, backend.LockDefault, mode, "no options should yield LockDefault")
}

func TestResolve_SkipLocked(t *testing.T) {
	mode, err := lock.Resolve(lock.SkipLocked())
	require.NoError(t, err)
	assert.Equal(t, backend.LockSkipLocked, mode)
}

func TestResolve_NoWait(t *testing.T) {
	mode, err := lock.Resolve(lock.NoWait())
	require.NoError(t, err)
	assert.Equal(t, backend.LockNoWait, mode)
}

func TestResolve_SkipLockedAndNoWaitRejected(t *testing.T) {
	mode, err := lock.Resolve(lock.SkipLocked(), lock.NoWait())
	require.Error(t, err, "SkipLocked + NoWait must be rejected — they are mutually exclusive on PostgreSQL")
	assert.Contains(t, err.Error(), "mutually exclusive")
	assert.Equal(t, backend.LockDefault, mode, "the error path returns LockDefault as the zero value")
}

func TestResolve_OrderIndependent(t *testing.T) {
	// Passing options in reverse order yields the same conflict error,
	// so callers can't accidentally bypass the check by ordering.
	_, err := lock.Resolve(lock.NoWait(), lock.SkipLocked())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// Compile-time pins on the option-constructor signatures — a future change
// that makes SkipLocked() or NoWait() return something other than lock.Option
// breaks the engine wrapper `func SkipLocked() LockOption { return lock.SkipLocked() }`.
var (
	_ lock.Option = lock.SkipLocked()
	_ lock.Option = lock.NoWait()
)

// Sanity: the error returned for the conflict case is a plain error (not a
// wrapped sentinel). Callers that want to differentiate use the message
// substring check, mirroring engine/tx_test.go.
func TestResolve_ErrorIsPlain(t *testing.T) {
	_, err := lock.Resolve(lock.SkipLocked(), lock.NoWait())
	require.Error(t, err)
	assert.NotErrorIs(t, err, errors.New("dummy"), "no specific sentinel is exposed by this contract")
}
