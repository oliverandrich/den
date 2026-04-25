// SPDX-License-Identifier: MIT

package s3

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/storage"
)

// minioImage pins the MinIO server image used by the testcontainers
// harness. Bumping it is a one-line change.
const minioImage = "minio/minio:RELEASE.2024-01-16T16-07-38Z"

var (
	sharedMinioOnce sync.Once
	sharedSetupErr  error
	sharedContainer *tcminio.MinioContainer
	sharedEndpoint  string
	sharedUser      string
	sharedPass      string
)

// startShared boots a single MinIO container reused across all tests
// in the package. Per-test buckets isolate data; the container is
// torn down once in TestMain.
func startShared(ctx context.Context) error {
	sharedMinioOnce.Do(func() {
		ctr, err := tcminio.Run(ctx, minioImage)
		if err != nil {
			sharedSetupErr = fmt.Errorf("starting MinIO container: %w", err)
			return
		}
		ep, err := ctr.ConnectionString(ctx)
		if err != nil {
			_ = tc.TerminateContainer(ctr)
			sharedSetupErr = fmt.Errorf("getting MinIO endpoint: %w", err)
			return
		}
		sharedContainer = ctr
		sharedEndpoint = ep
		sharedUser = ctr.Username
		sharedPass = ctr.Password
	})
	return sharedSetupErr
}

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedContainer != nil {
		_ = tc.TerminateContainer(sharedContainer)
	}
	os.Exit(code)
}

// bucketCounter generates collision-free bucket names so each test
// gets its own namespace on the shared container.
var bucketCounter atomic.Uint64

// newRawClient is the unwrapped minio-go client used by tests to
// MakeBucket / verify object placement directly, sidestepping the
// *Storage abstraction.
func newRawClient(t *testing.T) *miniogo.Client {
	t.Helper()
	c, err := miniogo.New(sharedEndpoint, &miniogo.Options{
		Creds:  credentials.NewStaticV4(sharedUser, sharedPass, ""),
		Secure: false,
		Region: "us-east-1",
	})
	require.NoError(t, err)
	return c
}

// newTestStorage spins up a fresh bucket on the shared MinIO and
// returns a *Storage targeting it. Skips the test if Docker isn't
// reachable (CI without docker-in-docker, dev box with daemon down).
// Extra options are appended after the connection options so callers
// can add WithPathPrefix etc. without losing the test wiring.
func newTestStorage(t *testing.T, extra ...Option) *Storage {
	t.Helper()
	ctx := t.Context()
	if err := startShared(ctx); err != nil {
		t.Skipf("MinIO testcontainer unavailable: %v", err)
	}

	bucket := fmt.Sprintf("den-test-%d", bucketCounter.Add(1))
	require.NoError(t, newRawClient(t).MakeBucket(ctx, bucket, miniogo.MakeBucketOptions{}))

	opts := make([]Option, 0, 4+len(extra))
	opts = append(opts,
		WithEndpoint(sharedEndpoint),
		WithSecure(false),
		WithRegion("us-east-1"),
		WithCredentials(sharedUser, sharedPass),
	)
	opts = append(opts, extra...)
	s, err := New(bucket, opts...)
	require.NoError(t, err)
	return s
}

func TestStorage_Store_WritesContentAddressedPath(t *testing.T) {
	s := newTestStorage(t)
	ctx := t.Context()

	a, err := s.Store(ctx, strings.NewReader("hello world"), ".txt", "text/plain")
	require.NoError(t, err)
	assert.Equal(t, int64(11), a.Size)
	assert.Len(t, a.SHA256, 64, "full SHA-256 hex")
	assert.Equal(t, "text/plain", a.Mime)
	assert.Contains(t, a.StoragePath, ".txt")
	assert.Equal(t,
		"b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"[:16],
		filepath.Base(a.StoragePath[:len(a.StoragePath)-len(".txt")]),
		"filename is first 16 hex of SHA-256",
	)
}

func TestStorage_Store_Dedupes(t *testing.T) {
	s := newTestStorage(t)
	ctx := t.Context()

	first, err := s.Store(ctx, strings.NewReader("identical"), ".bin", "application/octet-stream")
	require.NoError(t, err)

	second, err := s.Store(ctx, strings.NewReader("identical"), ".bin", "application/octet-stream")
	require.NoError(t, err)

	assert.Equal(t, first.StoragePath, second.StoragePath, "same bytes → same key")
	assert.Equal(t, first.SHA256, second.SHA256)
}

func TestStorage_Store_RejectsEmpty(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.Store(t.Context(), strings.NewReader(""), ".txt", "text/plain")
	require.ErrorIs(t, err, storage.ErrEmptyContent)
}

func TestStorage_Open_ReadsWhatStoreWrote(t *testing.T) {
	s := newTestStorage(t)
	ctx := t.Context()

	want := "the quick brown fox"
	a, err := s.Store(ctx, strings.NewReader(want), ".txt", "text/plain")
	require.NoError(t, err)

	obj, err := s.Open(ctx, a)
	require.NoError(t, err)
	t.Cleanup(func() { _ = obj.Close() })

	got, err := io.ReadAll(obj)
	require.NoError(t, err)
	assert.Equal(t, want, string(got))
}

func TestStorage_Delete(t *testing.T) {
	s := newTestStorage(t)
	ctx := t.Context()

	a, err := s.Store(ctx, strings.NewReader("delete me"), ".txt", "text/plain")
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, a))

	// minio-go's GetObject defers the missing-key error to the first
	// Read, not Open. ReadAll surfaces it.
	obj, err := s.Open(ctx, a)
	require.NoError(t, err)
	t.Cleanup(func() { _ = obj.Close() })
	_, err = io.ReadAll(obj)
	require.Error(t, err, "object must be gone after Delete")
}

func TestStorage_Delete_IdempotentOnMissing(t *testing.T) {
	s := newTestStorage(t)
	assert.NoError(t, s.Delete(t.Context(),
		document.Attachment{StoragePath: "never/existed.txt"}))
}

func TestStorage_URL_ReturnsPresignedURLThatWorks(t *testing.T) {
	s := newTestStorage(t)
	ctx := t.Context()

	want := "served via presigned"
	a, err := s.Store(ctx, strings.NewReader(want), ".txt", "text/plain")
	require.NoError(t, err)

	u := s.URL(a)
	require.NotEmpty(t, u)
	assert.Contains(t, u, "X-Amz-Signature",
		"presigned URL must carry an SigV4 signature")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, want, string(body))
}

func TestStorage_PathPrefix_NestsKeys(t *testing.T) {
	s := newTestStorage(t, WithPathPrefix("app42/"))
	ctx := t.Context()

	a, err := s.Store(ctx, strings.NewReader("nested"), ".txt", "text/plain")
	require.NoError(t, err)

	// The returned StoragePath is the bare relative form; the prefix is
	// applied transparently when talking to S3, so the round-trip must
	// still work without callers being aware of it.
	obj, err := s.Open(ctx, a)
	require.NoError(t, err)
	t.Cleanup(func() { _ = obj.Close() })
	body, err := io.ReadAll(obj)
	require.NoError(t, err)
	assert.Equal(t, "nested", string(body))

	// Direct StatObject via the raw client confirms the prefix landed
	// on the actual S3 key.
	_, err = newRawClient(t).StatObject(ctx, s.bucket, "app42/"+a.StoragePath, miniogo.StatObjectOptions{})
	require.NoError(t, err, "object should be stored under the path prefix")
}

func TestNew_RequiresBucket(t *testing.T) {
	_, err := New("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bucket name required")
}

// --- opener-side tests (still stubbed; full DSN parsing is bite 2) ---

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
