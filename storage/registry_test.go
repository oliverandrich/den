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
		_, err := storage.OpenURL(scheme + "://x")
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
			_, err := storage.OpenURL(scheme + "://x")
			assert.ErrorContains(t, err, "stub opener for "+scheme)
		})
	}
	wg.Wait()

	for i := range N / 2 {
		scheme := fmt.Sprintf("%s_late_%d", base, i)
		_, err := storage.OpenURL(scheme + "://x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stub opener for "+scheme)
	}
}

func TestStorageOpenURL_EmptyDSN(t *testing.T) {
	_, err := storage.OpenURL("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty DSN")
}

func TestStorageOpenURL_MissingScheme(t *testing.T) {
	_, err := storage.OpenURL("no-separator-here")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing scheme",
		"error should point at the missing :// separator")
}

func TestStorageOpenURL_UnregisteredScheme(t *testing.T) {
	_, err := storage.OpenURL("nosuch_scheme_" + regTestPrefix(t) + "://x")
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
		_, err := storage.OpenURL(dsn)
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

// capturingOpener returns an OpenerFunc that records the (location,
// urlPrefix) it was called with into the provided pointers. Used by the
// `?url_prefix=` extraction tests below to assert what the opener
// actually sees after OpenURL strips the query param.
func capturingOpener(loc, prefix *string) storage.OpenerFunc {
	return func(location, urlPrefix string) (den.Storage, error) {
		*loc = location
		*prefix = urlPrefix
		return nil, fmt.Errorf("captured")
	}
}

func TestStorageOpenURL_ExtractsURLPrefixFromQuery(t *testing.T) {
	scheme := regTestPrefix(t)
	var gotLocation, gotPrefix string
	storage.Register(scheme, capturingOpener(&gotLocation, &gotPrefix))

	_, err := storage.OpenURL(scheme + ":///uploads?url_prefix=/media")
	require.ErrorContains(t, err, "captured", "opener must be invoked")
	assert.Equal(t, "/media", gotPrefix,
		"url_prefix query param must be extracted and forwarded as the opener's urlPrefix arg")
}

func TestStorageOpenURL_StripsURLPrefixBeforeOpener(t *testing.T) {
	scheme := regTestPrefix(t)
	var gotLocation, gotPrefix string
	storage.Register(scheme, capturingOpener(&gotLocation, &gotPrefix))

	_, err := storage.OpenURL(scheme + "://bucket?region=us-east-1&url_prefix=/media&endpoint=foo")
	require.ErrorContains(t, err, "captured")
	assert.Equal(t, "/media", gotPrefix)
	assert.NotContains(t, gotLocation, "url_prefix",
		"opener must not see the url_prefix param in its location")
	assert.Contains(t, gotLocation, "region=us-east-1", "other query params survive")
	assert.Contains(t, gotLocation, "endpoint=foo", "other query params survive")
}

func TestStorageOpenURL_EmptyURLPrefixQueryParam(t *testing.T) {
	scheme := regTestPrefix(t)
	var gotLocation, gotPrefix string
	storage.Register(scheme, capturingOpener(&gotLocation, &gotPrefix))

	_, err := storage.OpenURL(scheme + ":///uploads?url_prefix=")
	require.ErrorContains(t, err, "captured")
	assert.Empty(t, gotPrefix,
		"empty url_prefix= treated same as not specified — empty string passed through")
}

// TestStorageOpenURL_StripsTrailingQuestionMark pins that when url_prefix
// is the only query param, the resulting location does NOT carry a
// dangling `?` — backends like file/file.go that string-manipulate the
// location would mishandle a trailing `?`.
func TestStorageOpenURL_StripsTrailingQuestionMark(t *testing.T) {
	scheme := regTestPrefix(t)
	var gotLocation, gotPrefix string
	storage.Register(scheme, capturingOpener(&gotLocation, &gotPrefix))

	_, err := storage.OpenURL(scheme + ":///uploads?url_prefix=/media")
	require.ErrorContains(t, err, "captured")
	assert.NotContains(t, gotLocation, "?",
		"empty leftover query string must not leave a dangling ? on the location")
}
