// SPDX-License-Identifier: MIT

// Package s3 is the S3 (and S3-compatible, e.g. MinIO) Storage backend
// for Den. The package is optional: Den core does not import it, so
// applications that don't `_`-import it pay nothing for the s3 code
// path (the linker drops it via dead-code elimination, and minio-go
// stays out of their binary even though it appears as a noted dep in
// go.sum).
//
// Importing this package for its side effect registers the "s3://"
// scheme with [storage.OpenURL]:
//
//	import _ "github.com/oliverandrich/den/storage/s3"
//
//	st, err := storage.OpenURL(
//	    "s3://my-bucket/uploads?region=eu-central-1&presign_ttl=15m")
//
// Or construct directly when an explicit configuration is preferable:
//
//	import s3 "github.com/oliverandrich/den/storage/s3"
//
//	st, err := s3.New("my-bucket",
//	    s3.WithRegion("eu-central-1"),
//	    s3.WithCredentials(accessKey, secretKey),
//	)
//
// DSN form: `s3://<bucket>[/<prefix>][?region=…&endpoint=…&secure=true|false&presign_ttl=15m]`.
// Credentials are intentionally NOT parsed from the DSN — they come
// from `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` env vars or the
// IAM instance profile.
//
// Object keys are content-addressed (`YYYY/MM/<sha256[:16]><ext>`)
// under an optional path prefix; identical bytes resolve to the same
// key, so two writers uploading the same file produce a single object.
//
// [Storage.URL] returns a SigV4-presigned GET URL valid for the
// configured TTL ([DefaultPresignTTL] by default). Because S3 URLs are
// absolute, [Storage] deliberately omits a `URLPrefix() string` method
// — HTTP-layer code can type-assert on that interface to decide
// whether to mount a local serving handler.
package s3

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/storage"
)

func init() {
	storage.Register("s3", openerFunc)
}

// openerFunc is the [storage.OpenerFunc] dispatched by [storage.OpenURL]
// for "s3://...". The full DSN form is
// `s3://<bucket>[/<prefix>][?region=…&endpoint=…&secure=true|false&presign_ttl=15m]`.
// Credentials are intentionally NOT parsed from the DSN — they come
// from AWS_* env vars or the IAM instance profile via the default
// chain in [New].
//
// A stray `?url_prefix=` query param (meaningful only to URL-prefix
// backends like file/, meaningless for S3 since S3 returns absolute
// URLs) is silently dropped by [parseDSN] alongside any other unknown
// query key.
func openerFunc(location string) (den.Storage, error) {
	bucket, opts, err := parseDSN(location)
	if err != nil {
		return nil, err
	}
	return New(bucket, opts...)
}

// parseDSN splits the location portion of "s3://<location>" into a
// bucket and the [Option] slice that mirrors its query parameters.
// Returns a labelled error for missing bucket, malformed query string,
// or invalid `secure` / `presign_ttl` values.
func parseDSN(location string) (string, []Option, error) {
	location, rawQuery, _ := strings.Cut(location, "?")
	bucket, prefixRaw, _ := strings.Cut(location, "/")
	if bucket == "" {
		return "", nil, errors.New("storage/s3: s3:// requires a bucket")
	}

	var opts []Option
	if prefix := strings.Trim(prefixRaw, "/"); prefix != "" {
		opts = append(opts, WithPathPrefix(prefix))
	}

	qs, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", nil, fmt.Errorf("storage/s3: invalid query string %q: %w", rawQuery, err)
	}
	if v := qs.Get("region"); v != "" {
		opts = append(opts, WithRegion(v))
	}
	if v := qs.Get("endpoint"); v != "" {
		opts = append(opts, WithEndpoint(v))
	}
	if v := qs.Get("secure"); v != "" {
		secure, err := strconv.ParseBool(v)
		if err != nil {
			return "", nil, fmt.Errorf("storage/s3: invalid secure=%q (want true/false): %w", v, err)
		}
		opts = append(opts, WithSecure(secure))
	}
	if v := qs.Get("presign_ttl"); v != "" {
		ttl, err := time.ParseDuration(v)
		if err != nil {
			return "", nil, fmt.Errorf("storage/s3: invalid presign_ttl=%q (want a Go duration like 15m): %w", v, err)
		}
		opts = append(opts, WithPresignTTL(ttl))
	}
	return bucket, opts, nil
}

// DefaultPresignTTL is the lifetime of URLs returned by [Storage.URL]
// when [WithPresignTTL] is not used.
const DefaultPresignTTL = 15 * time.Minute

// defaultEndpoint is the AWS S3 endpoint used when [WithEndpoint] is
// not supplied. Override for MinIO, localstack, or any S3-compatible
// service.
const defaultEndpoint = "s3.amazonaws.com"

// Option configures a [Storage] at construction. Pass to [New].
type Option func(*config)

type config struct {
	endpoint   string
	region     string
	secure     bool
	creds      *credentials.Credentials
	prefix     string
	presignTTL time.Duration
}

// WithEndpoint overrides the S3 endpoint. Format is "host" or
// "host:port" without a scheme; use [WithSecure] to toggle TLS.
// Default is AWS S3 ("s3.amazonaws.com").
func WithEndpoint(endpoint string) Option {
	return func(c *config) { c.endpoint = endpoint }
}

// WithRegion sets the AWS region (e.g. "eu-central-1"). Required by
// real S3; ignored by some S3-compatible services.
func WithRegion(region string) Option {
	return func(c *config) { c.region = region }
}

// WithSecure toggles HTTPS to the endpoint. Default is true.
func WithSecure(secure bool) Option {
	return func(c *config) { c.secure = secure }
}

// WithCredentials sets static AWS credentials. When omitted, the
// default chain "AWS_* env vars → IAM instance profile" is used,
// matching the standard AWS SDK behaviour.
func WithCredentials(accessKey, secretKey string) Option {
	return func(c *config) {
		c.creds = credentials.NewStaticV4(accessKey, secretKey, "")
	}
}

// WithPathPrefix nests all object keys under prefix inside the bucket.
// Useful for sharing one bucket across multiple applications. The
// prefix is applied transparently — [Storage.Store] still returns the
// bare relative path in the [document.Attachment.StoragePath].
func WithPathPrefix(prefix string) Option {
	return func(c *config) { c.prefix = prefix }
}

// WithPresignTTL sets the lifetime of URLs returned by [Storage.URL].
// Default is [DefaultPresignTTL].
func WithPresignTTL(ttl time.Duration) Option {
	return func(c *config) { c.presignTTL = ttl }
}

// Storage stores attachment bytes in an S3-compatible bucket. Object
// keys are content-addressed (`YYYY/MM/<sha256[:16]><ext>`) under an
// optional path prefix; identical bytes resolve to the same key.
type Storage struct {
	client     *miniogo.Client
	bucket     string
	prefix     string
	presignTTL time.Duration
}

// New constructs a [Storage] bound to bucket. Bucket existence is NOT
// probed — production buckets are typically created by IaC, and a
// per-Open round-trip is undesirable. Lookup errors surface lazily on
// the first Store / Open / Delete.
func New(bucket string, opts ...Option) (*Storage, error) {
	if bucket == "" {
		return nil, errors.New("storage/s3: bucket name required")
	}
	cfg := config{
		endpoint:   defaultEndpoint,
		secure:     true,
		presignTTL: DefaultPresignTTL,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.creds == nil {
		cfg.creds = credentials.NewChainCredentials([]credentials.Provider{
			&credentials.EnvAWS{},
			&credentials.IAM{},
		})
	}
	client, err := miniogo.New(cfg.endpoint, &miniogo.Options{
		Creds:  cfg.creds,
		Secure: cfg.secure,
		Region: cfg.region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage/s3: client init: %w", err)
	}
	return &Storage{
		client:     client,
		bucket:     bucket,
		prefix:     strings.Trim(cfg.prefix, "/"),
		presignTTL: cfg.presignTTL,
	}, nil
}

// Store implements [den.Storage]. Streams r into a temp file (to know
// size and hash before uploading), then PutObjects the temp file under
// `<prefix>/YYYY/MM/<sha256[:16]><ext>`. Skips the upload if a
// HeadObject sees the key already exists — identical bytes produce the
// same key, so a racing concurrent upload would write the same object.
func (s *Storage) Store(ctx context.Context, r io.Reader, ext, mime string) (document.Attachment, error) {
	tmp, err := os.CreateTemp("", "den-s3-upload-*")
	if err != nil {
		return document.Attachment{}, fmt.Errorf("storage/s3: creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(tmp, hasher), r)
	if closeErr := tmp.Close(); closeErr != nil && copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return document.Attachment{}, fmt.Errorf("storage/s3: writing temp file: %w", copyErr)
	}
	if size == 0 {
		return document.Attachment{}, storage.ErrEmptyContent
	}

	hashHex := hex.EncodeToString(hasher.Sum(nil))
	now := time.Now().UTC()
	relPath := path.Join(now.Format("2006"), now.Format("01"), hashHex[:16]+ext)
	key := s.objectKey(relPath)

	att := document.Attachment{
		StoragePath: relPath,
		Mime:        mime,
		Size:        size,
		SHA256:      hashHex,
	}

	if _, err := s.client.StatObject(ctx, s.bucket, key, miniogo.StatObjectOptions{}); err == nil {
		return att, nil
	} else if !isNotFound(err) {
		return document.Attachment{}, fmt.Errorf("storage/s3: head object %s: %w", key, err)
	}

	f, err := os.Open(tmpPath) //nolint:gosec // tmpPath comes from os.CreateTemp above — not user input
	if err != nil {
		return document.Attachment{}, fmt.Errorf("storage/s3: reopening temp file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := s.client.PutObject(ctx, s.bucket, key, f, size, miniogo.PutObjectOptions{
		ContentType: mime,
	}); err != nil {
		return document.Attachment{}, fmt.Errorf("storage/s3: put object %s: %w", key, err)
	}
	return att, nil
}

// Open implements [den.Storage]. The returned [io.ReadCloser] surfaces
// missing-key errors on first Read, not on Open — that's how
// minio-go's GetObject is wired.
func (s *Storage) Open(ctx context.Context, a document.Attachment) (io.ReadCloser, error) {
	key := s.objectKey(a.StoragePath)
	obj, err := s.client.GetObject(ctx, s.bucket, key, miniogo.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage/s3: get object %s: %w", key, err)
	}
	return obj, nil
}

// Delete implements [den.Storage]. Missing keys are treated as success
// — explicit NoSuchKey check covers S3-compatibles whose RemoveObject
// is not as forgiving as standard S3.
func (s *Storage) Delete(ctx context.Context, a document.Attachment) error {
	key := s.objectKey(a.StoragePath)
	err := s.client.RemoveObject(ctx, s.bucket, key, miniogo.RemoveObjectOptions{})
	if err == nil || isNotFound(err) {
		return nil
	}
	return fmt.Errorf("storage/s3: remove object %s: %w", key, err)
}

// URL implements [den.Storage]. Returns a SigV4-presigned GET URL
// valid for the configured presign TTL. Signing is local computation;
// any error here points at a programming bug (e.g. an invalid bucket
// name that slipped past New). Errors are logged and "" is returned so
// HTTP layers fall through to a 404 instead of crashing on a
// misconfiguration that has nothing to do with the request.
func (s *Storage) URL(a document.Attachment) string {
	key := s.objectKey(a.StoragePath)
	u, err := s.client.PresignedGetObject(context.Background(), s.bucket, key, s.presignTTL, nil)
	if err != nil {
		slog.Error("storage/s3: presign failed",
			"bucket", s.bucket, "key", key, "error", err)
		return ""
	}
	return u.String()
}

// objectKey nests relPath under the configured path prefix.
func (s *Storage) objectKey(relPath string) string {
	if s.prefix == "" {
		return relPath
	}
	return s.prefix + "/" + relPath
}

// isNotFound reports whether err is a "key does not exist" response
// from S3. minio-go surfaces this as ErrorResponse.Code == "NoSuchKey"
// regardless of which call produced the error.
func isNotFound(err error) bool {
	return miniogo.ToErrorResponse(err).Code == "NoSuchKey"
}
