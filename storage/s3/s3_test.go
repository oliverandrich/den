// SPDX-License-Identifier: MIT

package s3

import (
	"testing"

	"github.com/oliverandrich/den/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The submodule's first cut only wires the scheme into the registry.
// Real Store/Open/Delete behaviour lands in the follow-up commit that
// pulls in minio-go; until then OpenURL returns a clearly-labelled
// "not yet implemented" error so the dispatch is observable.

func TestInit_RegistersS3Scheme(t *testing.T) {
	_, err := storage.OpenURL("s3://my-bucket", "/media/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage/s3")
	assert.Contains(t, err.Error(), "not yet implemented")
	assert.Contains(t, err.Error(), "my-bucket",
		"error should echo the bucket so misconfig is visible")
}

func TestOpener_RequiresBucket(t *testing.T) {
	_, err := storage.OpenURL("s3://", "/media/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a bucket")
}

func TestOpener_ExtractsBucketFromLocation(t *testing.T) {
	cases := []string{
		"s3://my-bucket/prefix",
		"s3://my-bucket?region=eu-central-1",
		"s3://my-bucket/prefix?region=eu-central-1",
	}
	for _, dsn := range cases {
		t.Run(dsn, func(t *testing.T) {
			_, err := storage.OpenURL(dsn, "/media/")
			require.Error(t, err)
			assert.Contains(t, err.Error(), `bucket="my-bucket"`,
				"bucket extraction must stop at the first '/' or '?'")
		})
	}
}
