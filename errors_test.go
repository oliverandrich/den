package den_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
)

// TestInsertManyError_Unwrap_TracksFailureMutations pins the new contract:
// Unwrap is built fresh on each call, so callers that read Failures after
// mutating it see consistent unwrap output. The previous sync.Once cache
// silently went stale once Failures changed.
func TestInsertManyError_Unwrap_TracksFailureMutations(t *testing.T) {
	first := errors.New("boom-0")
	second := errors.New("boom-1")
	replaced := errors.New("replaced")

	e := &den.InsertManyError{
		Failures: []den.InsertFailure{
			{Index: 0, Err: first},
			{Index: 1, Err: second},
		},
		TotalFailures: 2,
	}

	u1 := e.Unwrap()
	require.Len(t, u1, 2)
	assert.Same(t, first, u1[0])
	assert.Same(t, second, u1[1])

	// Mutate Failures after the first Unwrap and confirm the next call
	// reflects the mutation. With the previous sync.Once cache this would
	// silently return the stale snapshot.
	e.Failures[0].Err = replaced
	u2 := e.Unwrap()
	assert.Same(t, replaced, u2[0],
		"Unwrap must reflect post-call mutations of Failures")
}

func TestInsertManyError_Error_TruncatesLongDetail(t *testing.T) {
	failures := make([]den.InsertFailure, 20)
	for i := range failures {
		failures[i] = den.InsertFailure{Index: i, Err: fmt.Errorf("boom-%d", i)}
	}
	e := &den.InsertManyError{Failures: failures, TotalFailures: 20}

	msg := e.Error()
	assert.Contains(t, msg, "20 failures", "header must report total count")
	// Render cap is 10; expect the first 10 detail lines plus an "and N more".
	assert.Contains(t, msg, "boom-0")
	assert.Contains(t, msg, "boom-9")
	assert.NotContains(t, msg, "boom-10", "detail beyond the render cap is elided")
	assert.Contains(t, msg, "10 more")
}

func TestInsertManyError_Error_NoTruncationUnderCap(t *testing.T) {
	failures := []den.InsertFailure{
		{Index: 0, Err: errors.New("boom-0")},
		{Index: 1, Err: errors.New("boom-1")},
		{Index: 2, Err: errors.New("boom-2")},
	}
	e := &den.InsertManyError{Failures: failures, TotalFailures: 3}

	msg := e.Error()
	assert.Contains(t, msg, "3 failures")
	assert.NotContains(t, msg, "more", "no truncation suffix when under the cap")
	assert.Contains(t, msg, "boom-0")
	assert.Contains(t, msg, "boom-2")
}

func TestInsertManyError_Error_TruncatedReportsTotal(t *testing.T) {
	// A truncated error (Failures shorter than TotalFailures) must still report
	// the uncapped count in its header, not the capped length.
	failures := make([]den.InsertFailure, 5)
	for i := range failures {
		failures[i] = den.InsertFailure{Index: i, Err: errors.New("boom")}
	}
	e := &den.InsertManyError{Failures: failures, Truncated: true, TotalFailures: 42}

	msg := e.Error()
	assert.Contains(t, msg, "42 failures", "header must use TotalFailures, not len(Failures)")
	assert.True(t, strings.Contains(msg, "truncated") || strings.Contains(msg, "more"),
		"truncated errors must signal truncation in the message")
}
