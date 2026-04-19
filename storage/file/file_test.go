// SPDX-License-Identifier: MIT

package file

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := New(t.TempDir(), "/media")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStorage_Store_WritesContentAddressedPath(t *testing.T) {
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

func TestStorage_Store_Dedupes(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	first, err := s.Store(ctx, strings.NewReader("identical"), ".bin", "application/octet-stream")
	require.NoError(t, err)

	second, err := s.Store(ctx, strings.NewReader("identical"), ".bin", "application/octet-stream")
	require.NoError(t, err)

	assert.Equal(t, first.StoragePath, second.StoragePath, "same bytes → same path")
	assert.Equal(t, first.SHA256, second.SHA256)
}

func TestStorage_Store_RejectsEmpty(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.Store(context.Background(), strings.NewReader(""), ".txt", "text/plain")
	require.ErrorIs(t, err, storage.ErrEmptyContent)
}

func TestStorage_Open_ReadsWhatStoreWrote(t *testing.T) {
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

func TestStorage_Open_PathTraversalRejected(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.Open(context.Background(), document.Attachment{StoragePath: "../escape.txt"})
	require.Error(t, err, "os.Root refuses paths that escape the root")
}

func TestStorage_Delete(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	a, err := s.Store(ctx, strings.NewReader("delete me"), ".txt", "text/plain")
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, a))

	_, err = s.Open(ctx, a)
	require.Error(t, err, "file must be gone after Delete")
}

func TestStorage_Delete_IdempotentOnMissing(t *testing.T) {
	s := newTestStorage(t)
	assert.NoError(t, s.Delete(context.Background(), document.Attachment{StoragePath: "never/existed.txt"}))
}

func TestStorage_URL(t *testing.T) {
	s := newTestStorage(t)

	assert.Equal(t, "/media/2026/04/abc.jpg",
		s.URL(document.Attachment{StoragePath: "2026/04/abc.jpg"}))
	assert.Equal(t, "/media/2026/04/abc.jpg",
		s.URL(document.Attachment{StoragePath: "/2026/04/abc.jpg"}),
		"leading slash tolerated")
}

func TestStorage_URL_TrimsPrefixSlash(t *testing.T) {
	s, err := New(t.TempDir(), "/media/")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	assert.Equal(t, "/media/a.jpg", s.URL(document.Attachment{StoragePath: "a.jpg"}))
}

func TestNew_CreatesRoot(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "does", "not", "exist", "yet")

	s, err := New(root, "/media")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestInit_RegistersFileScheme(t *testing.T) {
	// Side-effect import registers "file://" — OpenURL should dispatch here.
	// t.TempDir() is absolute, so we need the 4-slash (SQLAlchemy-style)
	// form: "file:///" + "/abs/path" → "file:////abs/path" → "/abs/path".
	tmp := t.TempDir()
	s, err := storage.OpenURL("file:///"+tmp, "/media")
	require.NoError(t, err)
	require.NotNil(t, s)

	sv, ok := s.(interface{ URLPrefix() string })
	require.True(t, ok)
	assert.Equal(t, "/media", sv.URLPrefix())

	if c, ok := s.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}

func TestInit_RelativePath(t *testing.T) {
	// 3-slash form: "file:///relative" → location "/relative" → strip → "relative".
	t.Chdir(t.TempDir())
	s, err := storage.OpenURL("file:///uploads", "/media")
	require.NoError(t, err)
	require.NotNil(t, s)
	if c, ok := s.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}

func TestInit_RejectsEmptyPath(t *testing.T) {
	_, err := storage.OpenURL("file://", "/media")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a path")

	_, err = storage.OpenURL("file:///", "/media")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a path")
}
