// SPDX-License-Identifier: MIT

// Package storage holds Den's reference Storage implementations.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oliverandrich/den/document"
)

// ErrEmptyContent is returned by Store when the reader yields zero bytes.
var ErrEmptyContent = errors.New("storage: refusing to store empty content")

// FilesystemStorage keeps attachment bytes on the local filesystem rooted
// at a configurable directory. Paths are year/month-bucketed with a
// filename derived from the first 16 hex digits of the content's SHA-256
// plus the caller-supplied extension.
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
type FilesystemStorage struct {
	root      *os.Root
	rootPath  string
	urlPrefix string
}

// NewFilesystemStorage creates the root directory if missing and returns a
// Storage bound to it. urlPrefix is the HTTP path prefix under which files
// are served (for example "/media"); a trailing slash is trimmed.
func NewFilesystemStorage(rootPath, urlPrefix string) (*FilesystemStorage, error) {
	if err := os.MkdirAll(rootPath, 0o750); err != nil {
		return nil, fmt.Errorf("creating storage root %q: %w", rootPath, err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("opening storage root %q: %w", rootPath, err)
	}
	return &FilesystemStorage{
		root:      root,
		rootPath:  rootPath,
		urlPrefix: strings.TrimRight(urlPrefix, "/"),
	}, nil
}

// Close releases the underlying file descriptor for the storage root.
func (s *FilesystemStorage) Close() error {
	return s.root.Close()
}

// URLPrefix returns the HTTP path prefix under which this storage's
// files are served (without a trailing slash). HTTP-layer packages
// (for example Burrow's contrib/uploads) use this to mount a serving
// handler on the same route the URL method produces.
//
// Remote Storage backends that return absolute URLs (S3, GCS, …) do
// NOT implement this method — HTTP-layer packages can type-assert on
// a "URLPrefix() string" interface to decide whether to register a
// local serving handler at all.
func (s *FilesystemStorage) URLPrefix() string {
	return s.urlPrefix
}

// Store implements den.Storage.
func (s *FilesystemStorage) Store(_ context.Context, r io.Reader, ext, mime string) (document.Attachment, error) {
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
		return document.Attachment{}, ErrEmptyContent
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

	// Dedup: if the target already exists, assume same hash → same content
	// and keep the existing file. Drop the temp.
	if _, err := os.Stat(absPath); err == nil {
		return document.Attachment{
			StoragePath: relPath,
			Mime:        mime,
			Size:        size,
			SHA256:      hashHex,
		}, nil
	}

	if err := os.Rename(tmpPath, absPath); err != nil {
		return document.Attachment{}, fmt.Errorf("moving %s into place: %w", absPath, err)
	}
	return document.Attachment{
		StoragePath: relPath,
		Mime:        mime,
		Size:        size,
		SHA256:      hashHex,
	}, nil
}

// Open implements den.Storage.
func (s *FilesystemStorage) Open(_ context.Context, a document.Attachment) (io.ReadCloser, error) {
	f, err := s.root.Open(filepath.FromSlash(a.StoragePath))
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", a.StoragePath, err)
	}
	return f, nil
}

// Delete implements den.Storage. Missing paths are treated as success.
func (s *FilesystemStorage) Delete(_ context.Context, a document.Attachment) error {
	err := s.root.Remove(filepath.FromSlash(a.StoragePath))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting %s: %w", a.StoragePath, err)
	}
	return nil
}

// URL implements den.Storage.
func (s *FilesystemStorage) URL(a document.Attachment) string {
	return s.urlPrefix + "/" + strings.TrimLeft(a.StoragePath, "/")
}
