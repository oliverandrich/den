package den

import (
	"context"
	"fmt"
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

// preflightAttachments enforces the storage.go contract that Den refuses
// to hard-delete attachment-bearing documents without a configured
// Storage. Returns ErrValidation when the doc has attachments and no
// Storage is installed; returns nil otherwise so callers can chain
// straight into the DB delete.
func (db *DB) preflightAttachments(rv reflect.Value) error {
	if db.storage != nil {
		return nil
	}
	attachments := collectAttachments(rv)
	if len(attachments) == 0 {
		return nil
	}
	return fmt.Errorf("%w: cannot hard-delete document with %d attachment(s) and no Storage configured",
		ErrValidation, len(attachments))
}

// cleanupAttachments removes the bytes behind a hard-deleted document.
// Assumes preflightAttachments already ran, so a missing Storage is a
// programmer error (caught via the preflight above) rather than a silent
// orphan.
//
// Remote Storage failures are logged and swallowed — the database delete
// has already succeeded and orphan bytes are recoverable via an offline
// sweep, whereas rolling back the DB delete would leave a broken
// document that references non-existent storage.
func (db *DB) cleanupAttachments(ctx context.Context, rv reflect.Value) {
	attachments := collectAttachments(rv)
	if len(attachments) == 0 {
		return
	}
	for _, a := range attachments {
		if err := db.storage.Delete(ctx, a); err != nil {
			slog.Warn("den: failed to delete attachment bytes",
				"path", a.StoragePath, "error", err)
		}
	}
}
