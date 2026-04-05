// Package id provides ULID-based unique identifier generation.
// This is a leaf package with no Den framework dependencies.
package id

import (
	"crypto/rand"

	"github.com/oklog/ulid/v2"
)

// New generates a new ULID string. ULIDs are lexicographically sortable
// and timestamp-ordered. Use this for document IDs, worker IDs, or any
// unique identifier.
func New() string {
	return ulid.MustNew(ulid.Now(), rand.Reader).String()
}
