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
