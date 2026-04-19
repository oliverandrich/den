package den

import (
	"context"
	"log/slog"
	"reflect"

	"github.com/oliverandrich/den/document"
)

// attachmentType is the cached reflect.Type of document.Attachment. Used by
// the delete-cascade walker to recognise attachment fields regardless of
// whether they are embedded, named, or reached via a pointer.
var attachmentType = reflect.TypeFor[document.Attachment]()

// collectAttachments returns every non-zero document.Attachment reachable
// from v. It walks into struct fields and pointer-to-struct fields, but
// does NOT follow slices or maps — those call for explicit handling.
//
// The walker stops recursing once it reaches an Attachment (so a nested
// Attachment inside an Attachment, which cannot be expressed anyway, would
// not be double-collected).
func collectAttachments(v reflect.Value) []document.Attachment {
	var out []document.Attachment
	walkAttachments(v, &out)
	return out
}

func walkAttachments(v reflect.Value, out *[]document.Attachment) {
	// Pointers: dereference if non-nil, otherwise there is nothing here.
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}

	// Exact match: we have found an Attachment.
	if v.Type() == attachmentType {
		att, ok := v.Interface().(document.Attachment)
		if ok && !att.IsZero() {
			*out = append(*out, att)
		}
		return
	}

	// Otherwise recurse into every field — embeds and named struct fields alike.
	for i := range v.NumField() {
		f := v.Field(i)
		switch f.Kind() { //nolint:exhaustive // only struct-shaped fields can hold an Attachment
		case reflect.Struct:
			walkAttachments(f, out)
		case reflect.Ptr:
			if f.Type().Elem().Kind() == reflect.Struct {
				walkAttachments(f, out)
			}
		}
	}
}

// cleanupAttachments is called immediately after a successful hard-delete
// to remove the bytes behind any document.Attachment fields on the doc.
//
// Failures are logged and swallowed — a missing Storage or a remote
// failure should not undo a successful database delete. Orphan bytes are
// recoverable via an offline sweep that cross-references filesystem paths
// against the remaining StoragePath values in the DB; an orphaned database
// reference to already-deleted bytes would be worse (broken link on the
// public site).
func (db *DB) cleanupAttachments(ctx context.Context, rv reflect.Value) {
	attachments := collectAttachments(rv)
	if len(attachments) == 0 {
		return
	}
	if db.storage == nil {
		slog.Warn("den: hard-deleting document with attachments but no Storage is configured; bytes are orphaned",
			"count", len(attachments))
		return
	}
	for _, a := range attachments {
		if err := db.storage.Delete(ctx, a); err != nil {
			slog.Warn("den: failed to delete attachment bytes",
				"path", a.StoragePath, "error", err)
		}
	}
}
