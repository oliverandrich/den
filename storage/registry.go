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
//	s, err := storage.OpenURL("file:///data/media?url_prefix=/media")
package storage

import (
	"errors"
	"fmt"
	"net/url"
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
// The scheme is normalized to lowercase before storage so registration
// and lookup stay case-insensitive, matching URL-scheme semantics:
// "file", "File", and "FILE" all address the same backend.
//
// Panics if a different opener is already registered for scheme —
// mirrors Den's database-backend registration semantics. Duplicate
// registrations surface mis-wiring (two backend packages claiming the
// same scheme, a replace-directive fork, or a manual call after a
// side-effect import) at process startup instead of at first lookup.
func Register(scheme string, opener OpenerFunc) {
	scheme = strings.ToLower(scheme)
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
// OpenerFunc registered for that scheme. The optional `url_prefix=…`
// query parameter on the location is intercepted here and passed to
// the opener as its second argument — backends that need a URL prefix
// (file) consume it; backends that return absolute URLs (s3) ignore it.
// The opener never sees `url_prefix` in its location.
//
// The scheme is matched case-insensitively: "file://...", "File://..."
// and "FILE://..." all resolve to the same backend.
//
// Returns an error with a helpful message for empty DSNs, missing
// schemes, or schemes without a registered opener — the last usually
// means a backend sub-package needs to be side-effect imported.
func OpenURL(dsn string) (den.Storage, error) {
	if dsn == "" {
		return nil, errors.New("storage: empty DSN")
	}
	scheme, location, ok := strings.Cut(dsn, "://")
	if !ok {
		return nil, fmt.Errorf("storage: missing scheme in DSN %q (expected <scheme>://<location>)", dsn)
	}
	scheme = strings.ToLower(scheme)
	registryMu.RLock()
	opener := registry[scheme]
	registryMu.RUnlock()
	if opener == nil {
		return nil, fmt.Errorf("storage: no backend registered for scheme %q (did you forget to import a backend sub-package?)", scheme)
	}
	location, urlPrefix := extractURLPrefix(location)
	return opener(location, urlPrefix)
}

// extractURLPrefix splits the optional `url_prefix=…` query parameter
// out of a DSN location, returning the location with that param removed
// and the value of the prefix. Other query parameters survive,
// re-encoded via [url.Values.Encode] (so they may be reordered
// alphabetically — opener implementations don't depend on order).
//
// A location with no query string, or a query string without
// `url_prefix`, is returned unchanged with an empty prefix. An empty
// value (`?url_prefix=`) is treated the same as not specified.
//
// Falls back to returning the location unchanged when the query string
// is malformed enough to fail [url.ParseQuery] — opener gets the raw
// location and can decide whether to error.
func extractURLPrefix(location string) (string, string) {
	base, rawQuery, hasQuery := strings.Cut(location, "?")
	if !hasQuery {
		return location, ""
	}
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return location, ""
	}
	if !q.Has("url_prefix") {
		return location, ""
	}
	prefix := q.Get("url_prefix")
	q.Del("url_prefix")
	encoded := q.Encode()
	if encoded == "" {
		return base, prefix
	}
	return base + "?" + encoded, prefix
}
