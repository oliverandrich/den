// SPDX-License-Identifier: MIT

// Package storage defines the Storage-backend registry used by
// [OpenURL] to construct a [den.Storage] from a URL-style DSN. Concrete
// backend implementations live in sub-packages and register themselves
// on import:
//
//	import (
//	    "github.com/oliverandrich/den/storage"
//	    _ "github.com/oliverandrich/den/storage/file" // registers file://
//	)
//
//	s, err := storage.OpenURL("file://./data/media", "/media/")
package storage

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/oliverandrich/den"
)

// ErrEmptyContent is returned by Storage implementations when a Store
// call receives zero bytes. Exposed here so HTTP-layer code can check
// for it regardless of which backend is in use.
var ErrEmptyContent = errors.New("storage: refusing to store empty content")

// OpenerFunc constructs a [den.Storage] from the location portion of a
// DSN (everything after the `<scheme>://`) plus the URL prefix under
// which the storage will be served. Registered per scheme via
// [Register].
type OpenerFunc func(location, urlPrefix string) (den.Storage, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]OpenerFunc{}
)

// Register associates an [OpenerFunc] with a URL scheme. Typical usage
// is from a backend sub-package's init():
//
//	func init() {
//	    storage.Register("file", openFileStorage)
//	}
//
// Panics if a different opener is already registered for scheme — mirrors
// Den's database-backend registration semantics and catches accidental
// double-imports at process startup instead of at first use.
func Register(scheme string, opener OpenerFunc) {
	if scheme == "" {
		panic("storage: Register with empty scheme")
	}
	if opener == nil {
		panic("storage: Register with nil opener for scheme " + scheme)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[scheme]; exists {
		panic("storage: duplicate registration for scheme " + scheme)
	}
	registry[scheme] = opener
}

// OpenURL parses dsn as "<scheme>://<location>" and delegates to the
// OpenerFunc registered for that scheme. urlPrefix is passed through as
// the public URL prefix under which the storage should serve files
// (ignored by backends that return absolute URLs).
//
// Returns an error with a helpful message for empty DSNs, missing
// schemes, or schemes without a registered opener — the last usually
// means a backend sub-package needs to be side-effect imported.
func OpenURL(dsn, urlPrefix string) (den.Storage, error) {
	if dsn == "" {
		return nil, errors.New("storage: empty DSN")
	}
	scheme, location, ok := strings.Cut(dsn, "://")
	if !ok {
		return nil, fmt.Errorf("storage: missing scheme in DSN %q (expected <scheme>://<location>)", dsn)
	}
	registryMu.RLock()
	opener := registry[scheme]
	registryMu.RUnlock()
	if opener == nil {
		return nil, fmt.Errorf("storage: no backend registered for scheme %q (did you forget to import a backend sub-package?)", scheme)
	}
	return opener(location, urlPrefix)
}
