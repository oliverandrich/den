package engine_test

import (
	"github.com/oliverandrich/den/engine"

	"bytes"
	"context"
	"io"
	"testing"

	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/storage/file"
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
	fs, err := file.New(tmp, "/media")
	require.NoError(t, err)
	t.Cleanup(func() { _ = fs.Close() })

	db := dentest.MustOpenWith(t, []document.Document{&mediaDoc{}}, []engine.Option{engine.WithStorage(fs)})

	att, err := fs.Store(ctx, bytes.NewReader([]byte("payload")), ".bin", "application/octet-stream")
	require.NoError(t, err)

	// Sanity: bytes exist.
	f, err := fs.Open(ctx, att)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, f)
	_ = f.Close()

	m := &mediaDoc{Attachment: att, AltText: "test"}
	require.NoError(t, engine.Save(ctx, db, m))

	// Hard-delete via the HardDelete option.
	require.NoError(t, engine.Delete(ctx, db, m, engine.HardDelete()))

	// Bytes are gone.
	_, err = fs.Open(ctx, att)
	require.Error(t, err, "file.Storage.Open must fail after cascade delete")
}

func TestHardDelete_CollectsBothNamedAttachments(t *testing.T) {
	ctx := context.Background()
	fs, err := file.New(t.TempDir(), "/media")
	require.NoError(t, err)
	t.Cleanup(func() { _ = fs.Close() })

	db := dentest.MustOpenWith(t, []document.Document{&productDoc{}}, []engine.Option{engine.WithStorage(fs)})

	hero, err := fs.Store(ctx, bytes.NewReader([]byte("hero-bytes")), ".jpg", "image/jpeg")
	require.NoError(t, err)
	thumb, err := fs.Store(ctx, bytes.NewReader([]byte("thumb-bytes")), ".jpg", "image/jpeg")
	require.NoError(t, err)

	p := &productDoc{Hero: hero, Thumbnail: thumb, Name: "Widget"}
	require.NoError(t, engine.Save(ctx, db, p))

	require.NoError(t, engine.Delete(ctx, db, p, engine.HardDelete()))

	for name, a := range map[string]document.Attachment{"hero": hero, "thumbnail": thumb} {
		_, err := fs.Open(ctx, a)
		assert.Error(t, err, "%s bytes must be gone after cascade", name)
	}
}

// gallery has a LinkDelete cascade to mediaDoc, which carries an attachment.
// The cascade must drop the linked doc's bytes from Storage, matching the
// top-level Delete path.
type gallery struct {
	document.Base
	Name string                `json:"name"`
	Hero engine.Link[mediaDoc] `json:"hero"`
}

func TestHardDelete_Cascade_CleansUpChildAttachment(t *testing.T) {
	ctx := context.Background()
	fs, err := file.New(t.TempDir(), "/media")
	require.NoError(t, err)
	t.Cleanup(func() { _ = fs.Close() })

	db := dentest.MustOpenWith(t,
		[]document.Document{&mediaDoc{}, &gallery{}},
		[]engine.Option{engine.WithStorage(fs)},
	)

	att, err := fs.Store(ctx, bytes.NewReader([]byte("hero-bytes")), ".jpg", "image/jpeg")
	require.NoError(t, err)
	m := &mediaDoc{Attachment: att, AltText: "hero"}
	require.NoError(t, engine.Save(ctx, db, m))

	g := &gallery{Name: "g", Hero: engine.NewLink(m)}
	require.NoError(t, engine.Save(ctx, db, g))

	// Reload g fresh so Hero.Value is nil — otherwise the outer
	// cleanupAttachments walk on the parent discovers the child's
	// attachment through the in-memory link value and hides the cascade
	// bug. This test is about the cascade path itself.
	reloaded, err := engine.FindByID[gallery](ctx, db, g.ID)
	require.NoError(t, err)
	require.False(t, reloaded.Hero.IsLoaded(), "sanity: link must be lazy")

	// Sanity: bytes exist before cascade.
	f, err := fs.Open(ctx, att)
	require.NoError(t, err, "attachment must exist before cascade")
	_ = f.Close()

	require.NoError(t, engine.Delete(ctx, db, reloaded, engine.WithLinkRule(engine.LinkDelete)))

	_, err = engine.FindByID[mediaDoc](ctx, db, m.ID)
	require.ErrorIs(t, err, engine.ErrNotFound, "linked mediaDoc must be cascade-deleted")

	_, err = fs.Open(ctx, att)
	assert.Error(t, err, "child attachment bytes must be cleaned up on cascade delete")
}

func TestHardDelete_WithoutStorage_Rejects(t *testing.T) {
	// Without a Storage configured, Den must refuse to hard-delete a
	// document carrying Attachment bytes — orphan bytes are worse than a
	// clear error (storage.go godoc contract).
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
	require.NoError(t, engine.Save(ctx, db, m))

	err := engine.Delete(ctx, db, m, engine.HardDelete())
	require.ErrorIs(t, err, engine.ErrValidation)

	// Doc must still exist — the DB delete should not run when preflight rejects.
	_, err = engine.FindByID[mediaDoc](ctx, db, m.ID)
	require.NoError(t, err, "preflight must reject before the DB delete")
}

func TestHardDelete_Cascade_WithoutStorage_Rejects(t *testing.T) {
	// Same contract on the cascade path: a LinkDelete that reaches an
	// attachment-bearing child with no Storage must error, not orphan.
	ctx := context.Background()
	db := dentest.MustOpen(t, &mediaDoc{}, &gallery{})

	m := &mediaDoc{
		Attachment: document.Attachment{
			StoragePath: "fake/path.bin",
			Mime:        "application/octet-stream",
			Size:        7,
			SHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}
	require.NoError(t, engine.Save(ctx, db, m))

	g := &gallery{Name: "g", Hero: engine.NewLink(m)}
	require.NoError(t, engine.Save(ctx, db, g))

	reloaded, err := engine.FindByID[gallery](ctx, db, g.ID)
	require.NoError(t, err)

	err = engine.Delete(ctx, db, reloaded, engine.WithLinkRule(engine.LinkDelete))
	require.ErrorIs(t, err, engine.ErrValidation)
}
