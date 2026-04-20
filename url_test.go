package den_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
)

// stubOpener returns an opener that refuses to construct a real Backend and
// echoes the scheme back in the error. Callers assert on that echo to prove
// their scheme's opener was invoked — no Backend implementation needed.
func stubOpener(scheme string) func(ctx context.Context, dsn string) (den.Backend, error) {
	return func(_ context.Context, _ string) (den.Backend, error) {
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
			den.RegisterBackend(scheme, stubOpener(scheme))
		})
	}
	wg.Wait()

	// Every scheme must be observable — proves no registration was lost to a
	// race between concurrent map writes.
	for i := range N {
		scheme := fmt.Sprintf("%s_%d", base, i)
		_, err := den.OpenURL(ctx, scheme+"://x")
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
		den.RegisterBackend(scheme, stubOpener(scheme))
	}

	var wg sync.WaitGroup
	// Writers: register the second half.
	for i := range N / 2 {
		scheme := fmt.Sprintf("%s_late_%d", base, i)
		wg.Go(func() {
			den.RegisterBackend(scheme, stubOpener(scheme))
		})
	}
	// Readers: hit the pre-registered half. Any race on the map shows up
	// under -race as a report.
	for i := range N / 2 {
		scheme := fmt.Sprintf("%s_pre_%d", base, i)
		wg.Go(func() {
			_, err := den.OpenURL(ctx, scheme+"://x")
			assert.ErrorContains(t, err, "stub opener for "+scheme)
		})
	}
	wg.Wait()

	// Late-registered schemes must also be observable afterwards.
	for i := range N / 2 {
		scheme := fmt.Sprintf("%s_late_%d", base, i)
		_, err := den.OpenURL(ctx, scheme+"://x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stub opener for "+scheme)
	}
}
