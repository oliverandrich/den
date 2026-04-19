// SPDX-License-Identifier: MIT

package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oliverandrich/den/document"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStorage(t *testing.T) *FilesystemStorage {
	t.Helper()
	s, err := NewFilesystemStorage(t.TempDir(), "/media")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestFilesystemStorage_Store_WritesContentAddressedPath(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

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

func TestFilesystemStorage_Store_Dedupes(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	first, err := s.Store(ctx, strings.NewReader("identical"), ".bin", "application/octet-stream")
	require.NoError(t, err)

	second, err := s.Store(ctx, strings.NewReader("identical"), ".bin", "application/octet-stream")
	require.NoError(t, err)

	assert.Equal(t, first.StoragePath, second.StoragePath, "same bytes → same path")
	assert.Equal(t, first.SHA256, second.SHA256)
}

func TestFilesystemStorage_Store_RejectsEmpty(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.Store(context.Background(), strings.NewReader(""), ".txt", "text/plain")
	require.ErrorIs(t, err, ErrEmptyContent)
}

func TestFilesystemStorage_Open_ReadsWhatStoreWrote(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	want := "the quick brown fox"
	a, err := s.Store(ctx, strings.NewReader(want), ".txt", "text/plain")
	require.NoError(t, err)

	f, err := s.Open(ctx, a)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	got, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, want, string(got))
}

func TestFilesystemStorage_Open_PathTraversalRejected(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.Open(context.Background(), document.Attachment{StoragePath: "../escape.txt"})
	require.Error(t, err, "os.Root refuses paths that escape the root")
}

func TestFilesystemStorage_Delete(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	a, err := s.Store(ctx, strings.NewReader("delete me"), ".txt", "text/plain")
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, a))

	_, err = s.Open(ctx, a)
	require.Error(t, err, "file must be gone after Delete")
}

func TestFilesystemStorage_Delete_IdempotentOnMissing(t *testing.T) {
	s := newTestStorage(t)
	assert.NoError(t, s.Delete(context.Background(), document.Attachment{StoragePath: "never/existed.txt"}))
}

func TestFilesystemStorage_URL(t *testing.T) {
	s := newTestStorage(t)

	assert.Equal(t, "/media/2026/04/abc.jpg",
		s.URL(document.Attachment{StoragePath: "2026/04/abc.jpg"}))
	assert.Equal(t, "/media/2026/04/abc.jpg",
		s.URL(document.Attachment{StoragePath: "/2026/04/abc.jpg"}),
		"leading slash tolerated")
}

func TestFilesystemStorage_URL_TrimsPrefixSlash(t *testing.T) {
	s, err := NewFilesystemStorage(t.TempDir(), "/media/")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	assert.Equal(t, "/media/a.jpg", s.URL(document.Attachment{StoragePath: "a.jpg"}))
}

func TestNewFilesystemStorage_CreatesRoot(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "does", "not", "exist", "yet")

	s, err := NewFilesystemStorage(root, "/media")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}
