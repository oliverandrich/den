package storage_test

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/storage"
)

// stubOpener returns an OpenerFunc that refuses to construct a real Storage
// and echoes the scheme in its error. Callers assert on that echo to prove
// their scheme's opener was invoked — no den.Storage implementation needed.
func stubOpener(scheme string) storage.OpenerFunc {
	return func(_ string, _ string) (den.Storage, error) {
		return nil, fmt.Errorf("stub opener for %s", scheme)
	}
}

var regTestCounter atomic.Uint64

// regTestPrefix builds a collision-free scheme prefix per test invocation.
// The counter suffix keeps `go test -count=N` safe against this registry's
// panic-on-duplicate semantics, and replacing `/` handles a future
// `t.Run(...)` wrapper.
func regTestPrefix(t *testing.T) string {
	t.Helper()
	base := strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
	return fmt.Sprintf("regtest_%s_%d", base, regTestCounter.Add(1))
}

func TestStorageRegister_ConcurrentDistinctSchemes(t *testing.T) {
	const N = 20
	base := regTestPrefix(t)

	var wg sync.WaitGroup
	for i := range N {
		scheme := fmt.Sprintf("%s_%d", base, i)
		wg.Go(func() {
			storage.Register(scheme, stubOpener(scheme))
		})
	}
	wg.Wait()

	for i := range N {
		scheme := fmt.Sprintf("%s_%d", base, i)
		_, err := storage.OpenURL(scheme+"://x", "/media/")
		require.Error(t, err, "scheme %s should be registered", scheme)
		assert.Contains(t, err.Error(), "stub opener for "+scheme)
	}
}

func TestStorageOpenURL_ConcurrentRegisterAndOpen(t *testing.T) {
	const N = 20
	base := regTestPrefix(t)

	for i := range N / 2 {
		scheme := fmt.Sprintf("%s_pre_%d", base, i)
		storage.Register(scheme, stubOpener(scheme))
	}

	var wg sync.WaitGroup
	for i := range N / 2 {
		scheme := fmt.Sprintf("%s_late_%d", base, i)
		wg.Go(func() {
			storage.Register(scheme, stubOpener(scheme))
		})
	}
	for i := range N / 2 {
		scheme := fmt.Sprintf("%s_pre_%d", base, i)
		wg.Go(func() {
			_, err := storage.OpenURL(scheme+"://x", "/media/")
			assert.ErrorContains(t, err, "stub opener for "+scheme)
		})
	}
	wg.Wait()

	for i := range N / 2 {
		scheme := fmt.Sprintf("%s_late_%d", base, i)
		_, err := storage.OpenURL(scheme+"://x", "/media/")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stub opener for "+scheme)
	}
}

func TestStorageOpenURL_EmptyDSN(t *testing.T) {
	_, err := storage.OpenURL("", "/media/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty DSN")
}

func TestStorageOpenURL_MissingScheme(t *testing.T) {
	_, err := storage.OpenURL("no-separator-here", "/media/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing scheme",
		"error should point at the missing :// separator")
}

func TestStorageOpenURL_UnregisteredScheme(t *testing.T) {
	_, err := storage.OpenURL("nosuch_scheme_"+regTestPrefix(t)+"://x", "/media/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no backend registered")
	assert.Contains(t, err.Error(), "did you forget to import a backend sub-package?",
		"error should nudge callers toward the side-effect import")
}

func TestStorageRegister_IsCaseInsensitive(t *testing.T) {
	scheme := regTestPrefix(t) + "_MixedCase"
	storage.Register(scheme, stubOpener(strings.ToLower(scheme)))

	// Lookup with the original case, all-lower, and all-upper must all resolve.
	for _, dsn := range []string{
		scheme + "://x",
		strings.ToLower(scheme) + "://x",
		strings.ToUpper(scheme) + "://x",
	} {
		_, err := storage.OpenURL(dsn, "/media/")
		require.Error(t, err, "registered scheme must resolve regardless of case: %s", dsn)
		assert.Contains(t, err.Error(), "stub opener for "+strings.ToLower(scheme))
	}
}

func TestStorageRegister_CaseInsensitiveDuplicatePanics(t *testing.T) {
	base := regTestPrefix(t) + "_dup"
	storage.Register(base, stubOpener(strings.ToLower(base)))

	// Different casing refers to the same normalized scheme — registration
	// must panic rather than silently coexist with the lowercase entry.
	require.Panics(t, func() {
		storage.Register(strings.ToUpper(base), stubOpener(strings.ToUpper(base)))
	})
}
