// SPDX-License-Identifier: MIT

// Package file provides a local-filesystem Storage backend for Den.
//
// Importing this package for its side effect registers the "file://"
// scheme with [storage.OpenURL]:
//
//	import _ "github.com/oliverandrich/den/storage/file"
//
//	s, err := storage.OpenURL("file:///data/media?url_prefix=/media")
//
// The DSN follows the SQLAlchemy/JDBC convention: three slashes for a
// relative path, four for an absolute path (one leading slash is
// stripped on parse so standard URL libraries see the whole location
// in the path component).
//
// For direct construction without the registry, call [New]; it takes
// the filesystem path literally.
package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/storage"
)

func init() {
	storage.Register("file", func(location string) (den.Storage, error) {
		// Strip `?url_prefix=` first — otherwise the trailing `?…`
		// ends up inside the filesystem path after the TrimPrefix below.
		path, urlPrefix := storage.URLPrefixFromLocation(location)
		// SQLAlchemy/JDBC-style convention matching the sqlite backend:
		// "file:///relative/path" → "relative/path"
		// "file:////absolute/path" → "/absolute/path"
		// A standard URL parser places everything in the path component
		// (authority stays empty in both forms); stripping one leading
		// slash yields the user-intended filesystem path.
		path = strings.TrimPrefix(path, "/")
		if path == "" {
			return nil, fmt.Errorf("storage/file: file:// requires a path")
		}
		return New(path, urlPrefix)
	})
}

// Storage keeps attachment bytes on the local filesystem rooted at a
// configurable directory. Paths are year/month-bucketed with a filename
// derived from the first 16 hex digits of the content's SHA-256 plus the
// caller-supplied extension.
//
// Example produced path: "2026/04/abc123def4567890.jpg".
//
// When two calls upload identical bytes in the same month, the path
// collides and the second Store short-circuits, returning the existing
// path without duplicating the file on disk. Content-addressing with the
// unique StoragePath index on the Den side gives atomic deduplication
// across application restarts.
//
// Path traversal in Open/Delete is prevented by os.Root (Go 1.24+), which
// refuses any path that escapes the configured root directory.
type Storage struct {
	root      *os.Root
	rootPath  string
	urlPrefix string
}

// New creates the root directory if missing and returns a Storage bound
// to it. urlPrefix is the HTTP path prefix under which files are served
// (for example "/media"); a trailing slash is trimmed.
func New(rootPath, urlPrefix string) (*Storage, error) {
	if err := os.MkdirAll(rootPath, 0o750); err != nil {
		return nil, fmt.Errorf("creating storage root %q: %w", rootPath, err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("opening storage root %q: %w", rootPath, err)
	}
	return &Storage{
		root:      root,
		rootPath:  rootPath,
		urlPrefix: strings.TrimRight(urlPrefix, "/"),
	}, nil
}

// Close releases the underlying file descriptor for the storage root.
func (s *Storage) Close() error {
	return s.root.Close()
}

// URLPrefix returns the HTTP path prefix under which this storage's
// files are served (without a trailing slash). HTTP-layer packages use
// this to mount a serving handler on the same route the URL method
// produces.
//
// Remote Storage backends that return absolute URLs (S3, GCS, …) do NOT
// implement this method — HTTP-layer packages can type-assert on a
// "URLPrefix() string" interface to decide whether to register a local
// serving handler at all.
func (s *Storage) URLPrefix() string {
	return s.urlPrefix
}

// Store implements den.Storage.
func (s *Storage) Store(_ context.Context, r io.Reader, ext, mime string) (document.Attachment, error) {
	tmp, err := os.CreateTemp(s.rootPath, "upload-*")
	if err != nil {
		return document.Attachment{}, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	hasher := sha256.New()
	tee := io.TeeReader(r, hasher)
	size, copyErr := io.Copy(tmp, tee)
	if closeErr := tmp.Close(); closeErr != nil && copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return document.Attachment{}, fmt.Errorf("writing to temp file: %w", copyErr)
	}
	if size == 0 {
		return document.Attachment{}, storage.ErrEmptyContent
	}

	hashHex := hex.EncodeToString(hasher.Sum(nil))
	now := time.Now().UTC()
	relPath := filepath.ToSlash(filepath.Join(
		now.Format("2006"),
		now.Format("01"),
		hashHex[:16]+ext,
	))
	absPath := filepath.Join(s.rootPath, filepath.FromSlash(relPath))

	if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
		return document.Attachment{}, fmt.Errorf("creating storage directory: %w", err)
	}

	// Identical bytes → identical hash → identical path, so any existing
	// destination is bit-identical by construction; fs.ErrExist is a
	// successful dedup hit.
	if err := os.Link(tmpPath, absPath); err != nil && !errors.Is(err, fs.ErrExist) {
		return document.Attachment{}, fmt.Errorf("linking %s into place: %w", absPath, err)
	}
	return document.Attachment{
		StoragePath: relPath,
		Mime:        mime,
		Size:        size,
		SHA256:      hashHex,
	}, nil
}

// Open implements den.Storage.
func (s *Storage) Open(_ context.Context, a document.Attachment) (io.ReadCloser, error) {
	f, err := s.root.Open(filepath.FromSlash(a.StoragePath))
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", a.StoragePath, err)
	}
	return f, nil
}

// Delete implements den.Storage. Missing paths are treated as success.
func (s *Storage) Delete(_ context.Context, a document.Attachment) error {
	err := s.root.Remove(filepath.FromSlash(a.StoragePath))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting %s: %w", a.StoragePath, err)
	}
	return nil
}

// URL implements den.Storage.
func (s *Storage) URL(a document.Attachment) string {
	return s.urlPrefix + "/" + strings.TrimLeft(a.StoragePath, "/")
}
