package engine_test

import (
	"github.com/oliverandrich/den/engine"

	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubOpener returns an opener that refuses to construct a real Backend and
// echoes the scheme back in the error. Callers assert on that echo to prove
// their scheme's opener was invoked — no Backend implementation needed.
func stubOpener(scheme string) func(ctx context.Context, dsn string) (engine.Backend, error) {
	return func(_ context.Context, _ string) (engine.Backend, error) {
		return nil, fmt.Errorf("stub opener for %s", scheme)
	}
}

var regTestCounter atomic.Uint64

// regTestPrefix builds a collision-free scheme prefix per test invocation.
// The counter suffix keeps `go test -count=N` safe against the storage
// registry's panic-on-duplicate semantics, and replacing `/` handles a
// future `t.Run(...)` wrapper. parseScheme lowercases before lookup, so
// the prefix is lowercased too.
func regTestPrefix(t *testing.T) string {
	t.Helper()
	base := strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
	return fmt.Sprintf("regtest_%s_%d", base, regTestCounter.Add(1))
}

func TestRegisterBackend_ConcurrentDistinctSchemes(t *testing.T) {
	const N = 20
	base := regTestPrefix(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := range N {
		scheme := fmt.Sprintf("%s_%d", base, i)
		wg.Go(func() {
			engine.RegisterBackend(scheme, stubOpener(scheme))
		})
	}
	wg.Wait()

	// Every scheme must be observable — proves no registration was lost to a
	// race between concurrent map writes.
	for i := range N {
		scheme := fmt.Sprintf("%s_%d", base, i)
		_, err := engine.OpenURL(ctx, scheme+"://x")
		require.Error(t, err, "scheme %s should be registered", scheme)
		assert.Contains(t, err.Error(), "stub opener for "+scheme,
			"scheme %s must resolve to its own opener", scheme)
	}
}

func TestRegisterBackend_ConcurrentRegisterAndOpen(t *testing.T) {
	const N = 20
	base := regTestPrefix(t)
	ctx := context.Background()

	// Pre-register the first half so the concurrent readers have something
	// to look up. The second half is registered concurrently with the reads.
	for i := range N / 2 {
		scheme := fmt.Sprintf("%s_pre_%d", base, i)
		engine.RegisterBackend(scheme, stubOpener(scheme))
	}

	var wg sync.WaitGroup
	// Writers: register the second half.
	for i := range N / 2 {
		scheme := fmt.Sprintf("%s_late_%d", base, i)
		wg.Go(func() {
			engine.RegisterBackend(scheme, stubOpener(scheme))
		})
	}
	// Readers: hit the pre-registered half. Any race on the map shows up
	// under -race as a report.
	for i := range N / 2 {
		scheme := fmt.Sprintf("%s_pre_%d", base, i)
		wg.Go(func() {
			_, err := engine.OpenURL(ctx, scheme+"://x")
			assert.ErrorContains(t, err, "stub opener for "+scheme)
		})
	}
	wg.Wait()

	// Late-registered schemes must also be observable afterwards.
	for i := range N / 2 {
		scheme := fmt.Sprintf("%s_late_%d", base, i)
		_, err := engine.OpenURL(ctx, scheme+"://x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stub opener for "+scheme)
	}
}

func TestOpenURL_EmptyDSN(t *testing.T) {
	_, err := engine.OpenURL(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty database URL")
}

func TestOpenURL_MissingScheme(t *testing.T) {
	_, err := engine.OpenURL(context.Background(), "no-separator-here")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing scheme",
		"error should point at the missing :// separator")
}

func TestOpenURL_UnregisteredScheme(t *testing.T) {
	_, err := engine.OpenURL(context.Background(), "nosuch_scheme_"+regTestPrefix(t)+"://x")
	require.Error(t, err)
	require.ErrorIs(t, err, engine.ErrUnsupportedScheme,
		"unregistered-scheme error must wrap ErrUnsupportedScheme so callers can match via errors.Is")
	assert.Contains(t, err.Error(), "did you import the backend package?",
		"error should nudge callers toward the side-effect import")
}

func TestRegisterBackend_PanicsOnEmptyScheme(t *testing.T) {
	require.PanicsWithValue(t, "den: RegisterBackend with empty scheme", func() {
		engine.RegisterBackend("", stubOpener("ignored"))
	})
}

func TestRegisterBackend_PanicsOnNilOpener(t *testing.T) {
	scheme := regTestPrefix(t) + "_nilopener"
	require.PanicsWithValue(t, "den: RegisterBackend with nil opener for scheme "+scheme, func() {
		engine.RegisterBackend(scheme, nil)
	})
}

func TestRegisterBackend_PanicsOnDuplicate(t *testing.T) {
	scheme := regTestPrefix(t) + "_dup"
	engine.RegisterBackend(scheme, stubOpener(scheme))

	require.PanicsWithValue(t, "den: duplicate registration for scheme "+scheme, func() {
		engine.RegisterBackend(scheme, stubOpener(scheme))
	})
}

func TestRegisterBackend_CaseInsensitiveDuplicatePanics(t *testing.T) {
	base := regTestPrefix(t) + "_casedup"
	engine.RegisterBackend(base, stubOpener(strings.ToLower(base)))

	require.PanicsWithValue(t, "den: duplicate registration for scheme "+strings.ToLower(base), func() {
		engine.RegisterBackend(strings.ToUpper(base), stubOpener(strings.ToUpper(base)))
	})
}

func TestRegisterBackend_IsCaseInsensitive(t *testing.T) {
	scheme := regTestPrefix(t) + "_MixedCase"
	engine.RegisterBackend(scheme, stubOpener(strings.ToLower(scheme)))

	// Lookup with the original case, all-lower, and all-upper must all resolve.
	for _, dsn := range []string{
		scheme + "://x",
		strings.ToLower(scheme) + "://x",
		strings.ToUpper(scheme) + "://x",
	} {
		_, err := engine.OpenURL(context.Background(), dsn)
		require.Error(t, err, "registered scheme must resolve regardless of case: %s", dsn)
		assert.Contains(t, err.Error(), "stub opener for "+strings.ToLower(scheme))
	}
}
