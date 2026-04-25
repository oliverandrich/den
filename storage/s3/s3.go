// SPDX-License-Identifier: MIT

// Package s3 is the S3 (and S3-compatible, e.g. MinIO) Storage backend
// for Den. It lives in its own Go module so that the minio-go dependency
// only enters the build of applications that actually import it — Den
// core stays free of any S3-specific transitive deps.
//
// Importing this package for its side effect registers the "s3://"
// scheme with [storage.OpenURL]:
//
//	import _ "github.com/oliverandrich/den/storage/s3"
//
//	s, err := storage.OpenURL("s3://my-bucket?region=eu-central-1", "/media/")
//
// The implementation lands incrementally: this revision wires the scheme
// into the registry and parses the DSN, but constructing a Storage
// returns a "not yet implemented" error. The minio-go-backed Store /
// Open / Delete / URL implementation follows in the next bean step.
//
// Versioned independently of Den core; release tags use the
// `storage/s3/vX.Y.Z` form per Go-submodule convention.
package s3

import (
	"fmt"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/storage"
)

func init() {
	storage.Register("s3", openerFunc)
}

// openerFunc is the [storage.OpenerFunc] dispatched by [storage.OpenURL]
// for "s3://...". The DSN follows
// `s3://<bucket>[/<prefix>][?region=…&endpoint=…]`. Currently only the
// bucket is extracted; the rest is reserved for the implementation step.
func openerFunc(location, _ string) (den.Storage, error) {
	bucket := bucketFromLocation(location)
	if bucket == "" {
		return nil, fmt.Errorf("storage/s3: s3:// requires a bucket (got %q)", location)
	}
	return nil, fmt.Errorf("storage/s3: not yet implemented (bucket=%q)", bucket)
}

// bucketFromLocation returns the bucket name from the location portion
// of the DSN — everything before the first '/' (path) or '?' (query).
func bucketFromLocation(location string) string {
	for i := range len(location) {
		if c := location[i]; c == '/' || c == '?' {
			return location[:i]
		}
	}
	return location
}
