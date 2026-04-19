package den_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mediaDoc is used by attachment-cascade tests: the Attachment embed is the
// subject of the walker.
type mediaDoc struct {
	document.Base
	document.Attachment
	AltText string `json:"alt_text"`
}

// productDoc carries two named Attachment fields to verify the walker
// collects both when a document has multiple attachments.
type productDoc struct {
	document.Base
	Hero      document.Attachment `json:"hero"`
	Thumbnail document.Attachment `json:"thumbnail"`
	Name      string              `json:"name"`
}

func TestHardDelete_CallsStorageDeleteOnAttachmentEmbed(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	fs, err := storage.NewFilesystemStorage(tmp, "/media")
	require.NoError(t, err)
	t.Cleanup(func() { _ = fs.Close() })

	db := dentest.MustOpenWith(t, []any{&mediaDoc{}}, []den.Option{den.WithStorage(fs)})

	att, err := fs.Store(ctx, bytes.NewReader([]byte("payload")), ".bin", "application/octet-stream")
	require.NoError(t, err)

	// Sanity: bytes exist.
	f, err := fs.Open(ctx, att)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, f)
	_ = f.Close()

	m := &mediaDoc{Attachment: att, AltText: "test"}
	require.NoError(t, den.Insert(ctx, db, m))

	// Hard-delete via the HardDelete option.
	require.NoError(t, den.Delete(ctx, db, m, den.HardDelete()))

	// Bytes are gone.
	_, err = fs.Open(ctx, att)
	require.Error(t, err, "FilesystemStorage.Open must fail after cascade delete")
}

func TestHardDelete_CollectsBothNamedAttachments(t *testing.T) {
	ctx := context.Background()
	fs, err := storage.NewFilesystemStorage(t.TempDir(), "/media")
	require.NoError(t, err)
	t.Cleanup(func() { _ = fs.Close() })

	db := dentest.MustOpenWith(t, []any{&productDoc{}}, []den.Option{den.WithStorage(fs)})

	hero, err := fs.Store(ctx, bytes.NewReader([]byte("hero-bytes")), ".jpg", "image/jpeg")
	require.NoError(t, err)
	thumb, err := fs.Store(ctx, bytes.NewReader([]byte("thumb-bytes")), ".jpg", "image/jpeg")
	require.NoError(t, err)

	p := &productDoc{Hero: hero, Thumbnail: thumb, Name: "Widget"}
	require.NoError(t, den.Insert(ctx, db, p))

	require.NoError(t, den.Delete(ctx, db, p, den.HardDelete()))

	for name, a := range map[string]document.Attachment{"hero": hero, "thumbnail": thumb} {
		_, err := fs.Open(ctx, a)
		assert.Error(t, err, "%s bytes must be gone after cascade", name)
	}
}

func TestHardDelete_WithoutStorage_DoesNotFail(t *testing.T) {
	// Simulate a setup where the user forgot to install Storage. The
	// cascade path should log a warning and still return nil — orphan
	// bytes are preferred over a failed delete that breaks callers.
	ctx := context.Background()
	db := dentest.MustOpen(t, &mediaDoc{})

	m := &mediaDoc{
		Attachment: document.Attachment{
			StoragePath: "fake/path.bin",
			Mime:        "application/octet-stream",
			Size:        7,
			SHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}
	require.NoError(t, den.Insert(ctx, db, m))
	require.NoError(t, den.Delete(ctx, db, m, den.HardDelete()))
}
