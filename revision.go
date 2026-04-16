package den

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"reflect"

	"github.com/oliverandrich/den/internal"
)

// IgnoreRevision returns a CRUDOption that skips revision checking.
func IgnoreRevision() CRUDOption {
	return func(o *crudOpts) {
		o.ignoreRevision = true
	}
}

func newRevision() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand.Read never returns error on supported platforms (Go 1.20+)
	return hex.EncodeToString(b)
}

func setRevision(v reflect.Value, info *internal.StructInfo, rev string) {
	revField := info.FieldByName("_rev")
	if revField == nil {
		return
	}
	v.FieldByIndex(revField.Index).SetString(rev)
}

func getRevision(v reflect.Value, info *internal.StructInfo) string {
	revField := info.FieldByName("_rev")
	if revField == nil {
		return ""
	}
	return v.FieldByIndex(revField.Index).String()
}

// checkAndUpdateRevision checks the stored revision matches, then sets a new one.
// Returns error if revision mismatch (unless ignoreRevision is set).
func checkAndUpdateRevision(ctx context.Context, db *DB, b ReadWriter, col *collectionInfo, rv reflect.Value, ignoreRevision bool) error {
	if !col.settings.UseRevision {
		return nil
	}

	id := getID(rv, col.structInfo)
	currentRev := getRevision(rv, col.structInfo)

	// Key the check off document existence (id), not on the in-memory rev.
	// An empty currentRev against a stored doc with a populated _rev is a
	// conflict — typically a caller constructed a doc without first loading
	// it, or read it via a path that did not populate _rev.
	if !ignoreRevision && id != "" {
		data, err := b.Get(ctx, col.meta.Name, id)
		if err != nil {
			return err
		}

		var partial struct {
			Rev string `json:"_rev"`
		}
		if err := db.decode(data, &partial); err != nil {
			return fmt.Errorf("decode for revision check: %w", err)
		}

		if partial.Rev != currentRev {
			return ErrRevisionConflict
		}
	}

	setRevision(rv, col.structInfo, newRevision())
	return nil
}
