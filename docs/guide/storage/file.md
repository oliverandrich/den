# File Storage Backend

`den/storage/file` is the reference Storage backend. It writes attachment bytes to the local filesystem under a configurable root directory, addressed by content hash.

## Construction

```go
import "github.com/oliverandrich/den/storage/file"

fs, err := file.New("./uploads", "/media")
```

- First argument: filesystem root where bytes are stored.
- Second argument: HTTP URL prefix for `Storage.URL()`.

Importing the package for its side effect also registers the `file://` scheme with `storage.OpenURL`, so configuration-driven setups can use either form interchangeably:

```go
import (
    "github.com/oliverandrich/den/storage"
    _ "github.com/oliverandrich/den/storage/file"
)

fs, err := storage.OpenURL("file:///uploads?url_prefix=/media")
```

## DSN syntax

The `file://` DSN follows the same SQLAlchemy/JDBC convention as `sqlite://`:

| DSN | Path handed to the filesystem |
|---|---|
| `file:///data/media` | `data/media` *(relative, 3 slashes)* |
| `file:////var/media` | `/var/media` *(absolute, 4 slashes)* |

One leading slash is stripped on parse so standard URL libraries (Go `net/url`, Python `urllib.parse`) can tokenise the DSN with the authority component staying empty. Direct construction via `file.New(...)` takes the filesystem path literally — no stripping.

The HTTP URL prefix is set via the `url_prefix` query parameter:

| DSN | `Storage.URL()` prefix |
|---|---|
| `file:///uploads?url_prefix=/media` | `/media` |
| `file:///uploads` | empty (URLs return as relative paths under the root) |

`url_prefix` is consumed by the storage registry and never reaches the file backend's parser, so other backends (S3) can honor or ignore the same query param uniformly.

## Object layout

The generated path is `YYYY/MM/<first-16-of-sha256><ext>` — grouped by month, content-addressed, dedup-on-write. Two uploads of the same bytes in the same month resolve to the same path; the second `Store` returns the existing path instead of duplicating the file on disk.

## Security-relevant behaviour

- **Path traversal is refused** — `Open` and `Delete` use `os.Root` (Go 1.24+). A `StoragePath` that escapes the root via `..` or symlinks cannot read anything outside the configured directory.
- **Empty uploads are rejected** — `Store` on a zero-byte reader returns `storage.ErrEmptyContent`.
- **Delete is idempotent** — a missing path returns success, simplifying cleanup orchestration against the document lifecycle.
- **Atomic dedup** — `Store` uses `os.Link` to install the temp upload at the final path; `fs.ErrExist` is treated as a successful dedup hit, which closes the read-then-rename TOCTOU window.

## URL-prefix accessor

The filesystem backend exposes its HTTP prefix via `URLPrefix() string`:

```go
fs, _ := file.New("./uploads", "/media")
fs.URLPrefix() // "/media"
```

HTTP-layer packages (`burrow/uploader`, custom handlers) use this to mount a serving handler on the same route that `URL` produces, without having the prefix configured twice. Remote backends (S3, GCS) intentionally do NOT implement this method — the absent `URLPrefix()` is the signal that the Storage is responsible for serving and the HTTP package should skip local routing.
