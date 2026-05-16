// SPDX-License-Identifier: MIT

package s3

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/storage"
)

// Fake credentials. gofakes3 does not verify signatures, but minio-go
// still requires *some* credentials to sign requests with.
const (
	fakeAccessKey = "den-test-access-key"
	fakeSecretKey = "den-test-secret-key"
)

var (
	sharedSetupOnce sync.Once
	sharedServer    *httptest.Server
	sharedEndpoint  string
)

// startShared boots one in-process gofakes3 server reused across all
// tests in the package. Per-test buckets isolate data; the server is
// torn down once in TestMain.
func startShared() {
	sharedSetupOnce.Do(func() {
		backend := s3mem.New()
		faker := gofakes3.New(backend)
		srv := httptest.NewServer(faker.Server())
		sharedServer = srv
		sharedEndpoint = srv.Listener.Addr().String()
	})
}

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedServer != nil {
		sharedServer.Close()
	}
	os.Exit(code)
}

var bucketCounter atomic.Uint64

// newRawClient is the unwrapped minio-go client used by tests to
// MakeBucket / verify object placement directly, sidestepping the
// *Storage abstraction.
func newRawClient(t *testing.T) *miniogo.Client {
	t.Helper()
	c, err := miniogo.New(sharedEndpoint, &miniogo.Options{
		Creds:  credentials.NewStaticV4(fakeAccessKey, fakeSecretKey, ""),
		Secure: false,
		Region: "us-east-1",
	})
	require.NoError(t, err)
	return c
}

// newTestStorage spins up a fresh bucket on the in-process gofakes3
// server and returns a *Storage targeting it. Extra options are
// appended after the connection options so callers can add
// WithPathPrefix etc. without losing the test wiring.
func newTestStorage(t *testing.T, extra ...Option) *Storage {
	t.Helper()
	startShared()

	bucket := fmt.Sprintf("den-test-%d", bucketCounter.Add(1))
	require.NoError(t, newRawClient(t).MakeBucket(t.Context(), bucket, miniogo.MakeBucketOptions{}))

	opts := make([]Option, 0, 4+len(extra))
	opts = append(opts,
		WithEndpoint(sharedEndpoint),
		WithSecure(false),
		WithRegion("us-east-1"),
		WithCredentials(fakeAccessKey, fakeSecretKey),
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

// --- opener + DSN-parsing tests ---

// applyOpts replays the [Option] slice into a config so tests can
// inspect what parseDSN actually wired up without round-tripping
// through New + an opaque *Storage.
func applyOpts(opts []Option) config {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func TestInit_RegistersS3Scheme(t *testing.T) {
	// Side-effect import + valid DSN now constructs a *Storage end-to-end
	// (no network round-trip — minio-go lazy-loads creds on first call).
	s, err := storage.OpenURL("s3://my-bucket")
	require.NoError(t, err)
	require.NotNil(t, s)

	st, ok := s.(*Storage)
	require.True(t, ok)
	assert.Equal(t, "my-bucket", st.bucket)
	assert.Empty(t, st.prefix)
	assert.Equal(t, DefaultPresignTTL, st.presignTTL)
}

func TestOpener_RequiresBucket(t *testing.T) {
	_, err := storage.OpenURL("s3://")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a bucket")
}

// TestOpener_IgnoresURLPrefixQueryParam pins that S3 silently accepts
// (and discards) the conventional `?url_prefix=` query param —
// matching the framework contract that url_prefix is meaningful only
// to URL-prefix-aware backends. parseDSN treats it as just another
// unknown query key.
func TestOpener_IgnoresURLPrefixQueryParam(t *testing.T) {
	s, err := storage.OpenURL("s3://my-bucket?url_prefix=/media&region=eu-central-1")
	require.NoError(t, err)
	st, ok := s.(*Storage)
	require.True(t, ok)
	assert.Equal(t, "my-bucket", st.bucket)
}

func TestParseDSN_BucketOnly(t *testing.T) {
	bucket, opts, err := parseDSN("my-bucket")
	require.NoError(t, err)
	assert.Equal(t, "my-bucket", bucket)
	assert.Empty(t, opts)
}

func TestParseDSN_PathPrefix(t *testing.T) {
	bucket, opts, err := parseDSN("my-bucket/some/prefix")
	require.NoError(t, err)
	assert.Equal(t, "my-bucket", bucket)
	cfg := applyOpts(opts)
	assert.Equal(t, "some/prefix", cfg.prefix)
}

func TestParseDSN_PathPrefix_TrimSlashes(t *testing.T) {
	_, opts, err := parseDSN("b/uploads/")
	require.NoError(t, err)
	assert.Equal(t, "uploads", applyOpts(opts).prefix,
		"trailing slash on the prefix should be trimmed")
}

func TestParseDSN_PathPrefix_EmptyAfterSlash(t *testing.T) {
	_, opts, err := parseDSN("b/")
	require.NoError(t, err)
	assert.Empty(t, opts, `"bucket/" with no prefix should produce no PathPrefix option`)
}

func TestParseDSN_QueryParams(t *testing.T) {
	bucket, opts, err := parseDSN("b?region=eu-central-1&endpoint=minio.local:9000&secure=false&presign_ttl=30m")
	require.NoError(t, err)
	assert.Equal(t, "b", bucket)

	cfg := applyOpts(opts)
	assert.Equal(t, "eu-central-1", cfg.region)
	assert.Equal(t, "minio.local:9000", cfg.endpoint)
	assert.False(t, cfg.secure)
	assert.Equal(t, 30*time.Minute, cfg.presignTTL)
}

func TestParseDSN_PathPrefixWithQueryParams(t *testing.T) {
	bucket, opts, err := parseDSN("b/uploads?region=eu-west-1")
	require.NoError(t, err)
	assert.Equal(t, "b", bucket)

	cfg := applyOpts(opts)
	assert.Equal(t, "uploads", cfg.prefix)
	assert.Equal(t, "eu-west-1", cfg.region)
}

func TestParseDSN_InvalidSecure(t *testing.T) {
	_, _, err := parseDSN("b?secure=maybe")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid secure="maybe"`)
}

func TestParseDSN_InvalidPresignTTL(t *testing.T) {
	_, _, err := parseDSN("b?presign_ttl=tomorrow")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid presign_ttl="tomorrow"`)
}

func TestParseDSN_InvalidQueryString(t *testing.T) {
	// `;` is rejected by net/url.ParseQuery as of Go 1.17+.
	_, _, err := parseDSN("b?region=us;invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid query string")
}

func TestOpener_AppliesDSNOptions(t *testing.T) {
	// End-to-end: OpenURL → parseDSN → New, with the parsed options
	// observable on the resulting *Storage.
	s, err := storage.OpenURL(
		"s3://my-bucket/uploads?endpoint=minio.local:9000&secure=false&presign_ttl=2h",
	)
	require.NoError(t, err)
	st, ok := s.(*Storage)
	require.True(t, ok)
	assert.Equal(t, "my-bucket", st.bucket)
	assert.Equal(t, "uploads", st.prefix)
	assert.Equal(t, 2*time.Hour, st.presignTTL)
}
