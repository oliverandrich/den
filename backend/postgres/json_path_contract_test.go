package postgres

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNoRawDataArrowOutsideHelper pins that every Postgres builder
// routes JSON field access through jsonbPath / jsonbPathText (in
// sql.go) rather than interpolating a raw `data->>'<field>'` string.
// The helpers translate dotted paths (`profile.bio`) into nested
// arrow chains (`jsonb_extract_path_text(data, 'profile', 'bio')`);
// a raw interpolation would silently treat dotted names as top-level
// keys and break nested-field support added in den-8f8t.
func TestNoRawDataArrowOutsideHelper(t *testing.T) {
	// `go test` runs with CWD = package directory, so reading "." here
	// scans backend/postgres/ regardless of where the test was invoked.
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	const arrowText = "data->>'"
	const arrowJSON = "data->'"
	// Files allowed to host raw arrow interpolation — i.e. the helpers
	// themselves. Add to this set if the helpers are ever extracted.
	allowedHelpers := map[string]bool{"sql.go": true}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if allowedHelpers[name] {
			continue
		}
		path := filepath.Join(".", name)
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		body := string(content)
		require.NotContainsf(t, body, arrowText,
			"%s: use jsonbPathText(field) instead of interpolating data->>'<field>' — see den-8f8t", name)
		require.NotContainsf(t, body, arrowJSON,
			"%s: use jsonbPath(field) instead of interpolating data->'<field>' — see den-8f8t", name)
	}
}
