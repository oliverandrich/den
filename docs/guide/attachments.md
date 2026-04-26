# Attachments & Storage

Den includes a built-in abstraction for attaching files to documents. The metadata (path, mime, size, hash) lives on an embeddable struct in the document; the actual bytes live behind a `den.Storage` interface that the application configures once at `Open` time.

This page covers the concept, the document-side embed, and the upload / read / delete workflow. The two shipped storage implementations and the contract for writing your own each have their own page:

- [File backend](storage/file.md) — local disk, content-addressed, included in Den core.
- [S3 backend](storage/s3.md) — S3 / S3-compatible (MinIO, localstack), shipped as a separate Go submodule so the minio-go dep doesn't enter applications that don't use it.
- [Writing a custom backend](storage/custom.md) — the `Storage` interface, required behaviour, optional `URLPrefix`.

!!! note "One Storage per DB"
    A single `den.Storage` is bound to the DB at Open. Every document type that uses attachments routes its bytes through that one backend. There is no per-collection storage routing built in — if you need that (public CDN for post covers, private bucket for invoices), wrap your Storage with a dispatcher in application code and pick a backend per call site.

## When to Use It

- Blog engines uploading post covers and images
- CMS attachments on pages
- User-avatar uploads
- Any document type that owns a file

If you only need to store small inline payloads (JSON config blobs, small snippets), just add a `string` or `[]byte` field. Attachments earn their keep when the payload is bytes that belong on a CDN, a disk, or S3 — not inside the document JSON.

## Enabling Attachments

Embed `document.Attachment` alongside `document.Base`:

```go
import "github.com/oliverandrich/den/document"

type Media struct {
    document.Base
    document.Attachment

    OriginalName string `json:"original_name" validate:"required,max=255"`
    AltText      string `json:"alt_text,omitempty"`
}
```

`Attachment` carries four fields, all validated via struct tags:

```go
type Attachment struct {
    StoragePath string `json:"storage_path"     validate:"required,max=1024"`
    Mime        string `json:"mime"             validate:"required,max=100"`
    Size        int64  `json:"size"             validate:"required,min=1"`
    SHA256      string `json:"sha256,omitempty" validate:"omitempty,len=64"`
}
```

These fields are set by the Storage when bytes are stored and are not meant to be edited by application code afterwards — `StoragePath`, `Size`, and `SHA256` are intrinsic to the stored content.

!!! note "About the `validate:` tags"
    The `validate:` tags fire only when validation is enabled on the DB via `validate.WithValidation()` (see [Validation](validation.md)). `Storage.Store` itself does NOT run validation; the tags kick in when the containing document is later inserted or updated through `den.Insert` / `den.Update`. With validation disabled the tags are inert metadata.

!!! note "StoragePath is an object key, not a URL"
    `StoragePath` is the path **relative to the storage backend's root** — for the file backend that is the root directory, for S3 that is the object key inside the bucket. Hosts, bucket names, query strings, and pre-signed URL parameters do NOT belong here; they come out of `Storage.URL()` on demand. The 1024-byte limit matches S3 and GCS object-key maxima.

## IS-a-file vs. HAS-files

There are two common shapes:

**IS-a-file** — the document represents a single file. Embed the `Attachment`:

```go
type Media struct {
    document.Base
    document.Attachment
    AltText string `json:"alt_text,omitempty"`
}
```

**HAS-files** — the document references one or more files. Use named fields:

```go
type Product struct {
    document.Base
    Name      string              `json:"name"`
    Hero      document.Attachment `json:"hero"`
    Thumbnail document.Attachment `json:"thumbnail"`
}
```

Both shapes use the same `Attachment` struct. Den's hard-delete cascade finds attachments in either position via reflection.

## Installing a Storage Backend

Storage is configured once, at `Open`, via `den.WithStorage`. Concrete backends live in sub-packages of `den/storage` (or a separate Go submodule, in the case of `storage/s3`) and register themselves on import. Two construction styles work:

**Direct constructor** — import the backend package and call its `New`:

```go
import (
    "github.com/oliverandrich/den"
    "github.com/oliverandrich/den/storage/file"
)

fs, err := file.New("./uploads", "/media")
if err != nil {
    return err
}

db, err := den.OpenURL(ctx, dsn, den.WithStorage(fs))
```

**DSN-based dispatch** — useful for configuration-driven setups where the backend is chosen at runtime (for example Burrow's `--storage-dsn` flag). Import the backend for its side effect so it registers its scheme:

```go
import (
    "github.com/oliverandrich/den"
    "github.com/oliverandrich/den/storage"
    _ "github.com/oliverandrich/den/storage/file" // registers "file://"
)

fs, err := storage.OpenURL("file:///uploads", "/media")
if err != nil {
    return err
}

db, err := den.OpenURL(ctx, dsn, den.WithStorage(fs))
```

For the per-backend DSN syntax, see the [file backend](storage/file.md#dsn-syntax) and [S3 backend](storage/s3.md#dsn-form) pages.

Without a Storage, `Store` / `Open` / `Delete` on attachments still work because application code holds a reference to the Storage instance directly. What breaks is the automatic hard-delete cascade, which only runs if a Storage is installed on the DB — `den.Delete(..., HardDelete())` on an attachment-bearing document returns `ErrValidation` instead of orphaning bytes.

## Uploading Bytes

Use `Storage.Store` directly — attachment upload happens in your HTTP handler (or CLI importer), not inside Den:

```go
func uploadHandler(db *den.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        f, header, err := r.FormFile("file")
        if err != nil { /* respond 400 */ return }
        defer f.Close()

        ext := filepath.Ext(header.Filename)
        mime := header.Header.Get("Content-Type")

        att, err := db.Storage().Store(r.Context(), f, ext, mime)
        if err != nil { /* respond 500 */ return }

        media := &Media{
            Attachment:   att,
            OriginalName: header.Filename,
        }
        if err := den.Insert(r.Context(), db, media); err != nil {
            // Clean up the stored bytes if the DB insert fails —
            // otherwise you have an orphan.
            _ = db.Storage().Delete(r.Context(), att)
            return
        }

        /* respond 201 with media */
    }
}
```

Two cleanup situations to keep in mind:

1. **Insert fails after Store succeeds** — application code must call `Storage.Delete` to avoid an orphan. Store-then-Insert is not atomic.
2. **Hard delete cascade** — Den handles this automatically: when `den.Delete(ctx, db, doc, den.HardDelete())` removes a document that contains attachments, Den calls `Storage.Delete` for each.

Hard-deleting an attachment-bearing document without a configured Storage is rejected with `ErrValidation` before the database delete runs — orphan bytes are worse than a clear error.

Remote `Storage.Delete` failures *after* the database delete succeeds are logged via `slog.Warn` but do not roll the delete back. A broken reference (DB pointing at missing bytes) is worse than orphan bytes, which are recoverable via an offline sweep that cross-references filesystem paths with `StoragePath` values.

## Reading Bytes

```go
f, err := db.Storage().Open(ctx, media.Attachment)
if err != nil { /* 404 */ return }
defer f.Close()

// Stream to HTTP response, copy to another storage, etc.
io.Copy(w, f)
```

## Serving URLs

`Storage.URL` returns the URL at which the file is served. For the filesystem backend that is the URL prefix passed at construction plus the storage path:

```go
fs, _ := file.New("./uploads", "/media")
// Attachment.StoragePath = "2026/04/abc123def4567890.jpg"
fs.URL(att) // -> "/media/2026/04/abc123def4567890.jpg"
```

Remote storage backends may return absolute URLs (`https://cdn.example.com/...`) or pre-signed URLs (S3 returns SigV4-presigned GETs). Applications should treat the return value as opaque.

Serving the files is up to the application. The [burrow/uploader](https://github.com/oliverandrich/burrow/tree/main/uploader) package provides a ready-made HTTP serving handler and multipart ingress helpers built on the `Storage` interface.

## Hard-Delete Cascade

When a document that contains attachments is hard-deleted, Den walks the document via reflection, collects every non-zero `Attachment` it finds (in embeds and in named fields), and calls `Storage.Delete` for each:

```go
// Bytes AND record are gone after this call.
err := den.Delete(ctx, db, media, den.HardDelete())
```

The walker only descends into struct fields and pointer-to-struct fields. Slices and maps are not followed — if you need to clean up attachments in a slice, use a `BeforeDeleter` hook:

```go
type Gallery struct {
    document.Base
    Items []document.Attachment `json:"items"`
}

func (g *Gallery) BeforeDelete(ctx context.Context) error {
    // Hard-delete cascade does not follow slices. Clean up by hand.
    storage := /* access via a closure or service locator */
    for _, a := range g.Items {
        if err := storage.Delete(ctx, a); err != nil {
            return err
        }
    }
    return nil
}
```

Soft-delete does NOT trigger the cascade — the bytes stay until you hard-delete. That matches the intent of soft delete (reversible removal).

## Uniqueness Trade-off

`document.Attachment` deliberately does NOT carry a `den:"unique"` tag on `StoragePath`. The reason: a `Product.Hero` attachment that references the same bytes as another `Product.Hero` is a legitimate case — two products can share a hero image via content addressing. A unique constraint on `storage_path` at the collection level would forbid that.

For "library" collections where each file must map to one record (a media library: one record per file), either add your own unique constraint in application logic (look up by SHA256 before insert) or lean on the content-addressed Storage's dedup — identical bytes produce the same `StoragePath`, and the database insert will fail if your collection has a unique index on that field.
