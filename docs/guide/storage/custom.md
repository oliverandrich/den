# Writing a Custom Storage Backend

Implement `den.Storage` to plug in any byte store you like — a CDN, a network share, an in-memory test stub, GCS or Azure Blob (until they ship as official packages).

## The interface

```go
type Storage interface {
    Store(ctx context.Context, r io.Reader, ext, mime string) (document.Attachment, error)
    Open(ctx context.Context, a document.Attachment) (io.ReadCloser, error)
    Delete(ctx context.Context, a document.Attachment) error
    URL(a document.Attachment) string
}
```

Install via `den.WithStorage(yourBackend)` at Open time, or register it for DSN dispatch by calling `storage.Register("myscheme", openerFn)` in your package's `init()`.

## Required behaviour

Every implementation must honour:

- **Content-addressed** — two `Store` calls with identical bytes must resolve to the same `StoragePath`. Den relies on this for dedup.
- **Idempotent Delete** — a missing path returns `nil`, not an error.
- **Concurrency-safe** — `Store` / `Open` / `Delete` / `URL` must be callable from multiple goroutines.
- **Fill in `SHA256`** — the returned `Attachment.SHA256` should be the full hex-encoded SHA-256 of the stored bytes. Several Den features (change tracking, dedup) rely on the hash.
- **Reject empty content** — `Store` on a zero-byte reader must return `storage.ErrEmptyContent` so the caller can map it to an HTTP 400 without parsing the message.

## Optional `URLPrefix`

```go
type URLPrefixer interface {
    URLPrefix() string
}
```

Implement this **only** when `URL` returns a path relative to the current HTTP server (i.e. the application is expected to serve the bytes itself, like the file backend does). HTTP-layer packages such as `burrow/uploader` type-assert on the local interface to decide whether to mount a serving handler and at what route. Backends that return absolute URLs (S3, GCS, a CDN) should omit the method — the absent `URLPrefix()` is the signal that the Storage serves itself.

## Reference implementations

The two shipped backends are good starting points:

- [`storage/file`](file.md) — local-disk implementation, ~120 lines. Use as a template for any filesystem-shaped backend (NFS mount, S3FS).
- [`storage/s3`](s3.md) — minio-go-backed remote implementation, optional package — Den core does not import it. Use as a template for any HTTP-shaped backend.
