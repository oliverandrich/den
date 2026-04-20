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

func TestInsertManyError_Unwrap_CachesBackingSlice(t *testing.T) {
	failures := []den.InsertFailure{
		{Index: 0, Err: errors.New("boom-0")},
		{Index: 1, Err: errors.New("boom-1")},
	}
	e := &den.InsertManyError{Failures: failures, TotalFailures: 2}

	u1 := e.Unwrap()
	u2 := e.Unwrap()
	require.Len(t, u1, 2)
	require.Len(t, u2, 2)

	// Same backing array — the slice is built once and reused across calls so
	// repeated errors.Is / errors.As walks don't re-allocate.
	assert.Same(t, &u1[0], &u2[0], "Unwrap must return the same backing array on repeat calls")
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
