package den

import (
	"fmt"
	"strings"
	"sync"
)

var (
	urlOpenersMu sync.RWMutex
	urlOpeners   = map[string]func(string) (Backend, error){}
)

// RegisterBackend registers a backend opener for a URL scheme.
// Called by backend packages in their init() functions.
func RegisterBackend(scheme string, opener func(dsn string) (Backend, error)) {
	urlOpenersMu.Lock()
	defer urlOpenersMu.Unlock()
	urlOpeners[scheme] = opener
}

// OpenURL opens a database connection using a URL-style DSN.
// Supported schemes depend on which backend packages are imported:
//   - sqlite:///path/to/db — import _ "github.com/oliverandrich/den/backend/sqlite"
//   - sqlite://:memory: — SQLite in-memory database
//   - postgres://user:pass@host:5432/db — import _ "github.com/oliverandrich/den/backend/postgres"
//   - postgresql://user:pass@host/db — PostgreSQL (alias)
//
// Backend packages register themselves automatically via init().
func OpenURL(dsn string, opts ...Option) (*DB, error) {
	scheme, err := parseScheme(dsn)
	if err != nil {
		return nil, err
	}

	urlOpenersMu.RLock()
	opener, ok := urlOpeners[scheme]
	urlOpenersMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("den: unsupported database scheme %q (did you import the backend package?)", scheme)
	}

	backend, err := opener(dsn)
	if err != nil {
		return nil, err
	}
	return open(backend, opts...)
}

// parseScheme extracts the scheme from a DSN string.
func parseScheme(dsn string) (string, error) {
	if dsn == "" {
		return "", fmt.Errorf("den: empty database URL")
	}
	scheme, _, ok := strings.Cut(dsn, "://")
	if !ok {
		return "", fmt.Errorf("den: invalid database URL %q (missing scheme, use sqlite:// or postgres://)", dsn)
	}
	return strings.ToLower(scheme), nil
}
