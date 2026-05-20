package core

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/internal/util"
	"github.com/oliverandrich/den/validate"
)

// runPrePersistHooks runs the mutating hook chain, struct-tag constraint
// check, and custom Validate hook that every insert and update path
// executes before touching the backend. Pick the right BeforeInsert /
// BeforeUpdate branch via isInsert.
//
// Order is load-bearing:
//   - Mutating hooks run first so they can populate defaults, compute
//     derived fields, and normalize values before validation sees them.
//   - validate.Document runs next — `validate:` struct-tag constraints
//     check the final post-hook state. Always-on, not opt-in: a doc
//     with constraint tags has those constraints enforced by Den itself.
//     Short-circuited when the registered type has no validate: tags
//     anywhere in its reachable type tree (col.structInfo.HasValidateTags
//     is false) — the reflective walk is the hottest allocator on the
//     write path and skipping it for tagless types is the dominant
//     observed win in profiles.
//   - Custom Validator.Validate() runs last so it can perform cross-field
//     checks against that same post-hook state.
//
// Every doc reaching this path has been through Register, which rejects
// types that don't embed document.Base — the document.Document assertion
// below is therefore guaranteed to hold and used purely to satisfy the
// typed signature on validate.Document.
func runPrePersistHooks(ctx context.Context, col *collectionInfo, doc any, isInsert bool) error {
	if isInsert {
		if err := runBeforeInsertHooks(ctx, doc); err != nil {
			return err
		}
	} else {
		if err := runBeforeUpdateHooks(ctx, doc); err != nil {
			return err
		}
	}
	if col.structInfo.HasValidateTags {
		d, ok := doc.(document.Document)
		if !ok {
			return fmt.Errorf("%w: type %T does not embed document.Base", ErrValidation, doc)
		}
		if err := validate.Document(d); err != nil {
			return fmt.Errorf("%w: %w", ErrValidation, err)
		}
	}
	return runValidationHooks(ctx, doc)
}

// writeDocCore is the shared single-doc write body used by insertCore,
// updateCore, and cascade link-writes (saveSingleLinkedValue). Runs the
// full persist chain in canonical order: pre-persist hooks → revision
// handling → base field stamp → encode → Put → snapshot → after-hooks.
// The branch is driven by isInsert.
//
// Order is load-bearing: hooks fire first so they can populate defaults;
// revision handling next so encoded bytes carry the right _rev;
// setBaseFields stamps _id and timestamps last; encode captures the final
// post-mutation state.
//
// Does NOT handle cascade-write or transaction wrapping — those decisions
// pre-empt the write body and stay at the caller (updateCore wraps in a
// tx for revision atomicity; cascadeWriteLinks runs ahead of the parent's
// prepare chain).
func writeDocCore(ctx context.Context, db *DB, b ReadWriter, target any, col *collectionInfo, isInsert, ignoreRevision bool) error {
	tv := reflect.ValueOf(target).Elem()

	if err := runPrePersistHooks(ctx, col, target, isInsert); err != nil {
		return err
	}

	if col.settings.UseRevision {
		if isInsert {
			setRevision(tv, col.structInfo, newRevision())
		} else if err := checkAndUpdateRevision(ctx, db, b, col, tv, ignoreRevision); err != nil {
			return err
		}
	}

	setBaseFields(tv, col.structInfo, time.Now(), isInsert)

	data, err := db.encode(target)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	id := getID(tv, col.structInfo)
	if err := b.Put(ctx, col.meta.Name, id, data); err != nil {
		return err
	}
	captureSnapshot(data, target)

	if isInsert {
		return runAfterInsertHooks(ctx, target)
	}
	return runAfterUpdateHooks(ctx, target)
}

// upsertResult bundles QuerySet.upsertOne's two return values so the
// runOnScope helper (which is single-valued over T) can carry both
// across the tx dispatch.
type upsertResult[T any] struct {
	doc      *T
	inserted bool
}

// encode and decode are the single JSON seam every storage write/read flows
// through. Kept as DB methods so every call site reads uniformly and any
// future encoder swap stays single-site.
func (db *DB) encode(v any) ([]byte, error)    { return json.Marshal(v) }
func (db *DB) decode(data []byte, v any) error { return json.Unmarshal(data, v) }

func setBaseFields(v reflect.Value, info *util.StructInfo, now time.Time, isInsert bool) {
	if idField := info.BaseID; idField != nil {
		fv := v.FieldByIndex(idField.Index)
		if fv.String() == "" {
			fv.SetString(NewID())
		}
	}

	nowVal := reflect.ValueOf(now)

	if isInsert {
		if createdField := info.BaseCreatedAt; createdField != nil {
			fv := v.FieldByIndex(createdField.Index)
			if fv.IsZero() {
				fv.Set(nowVal)
			}
		}
	}

	if updatedField := info.BaseUpdatedAt; updatedField != nil {
		v.FieldByIndex(updatedField.Index).Set(nowVal)
	}
}

func getID(v reflect.Value, info *util.StructInfo) string {
	idField := info.BaseID
	if idField == nil {
		return ""
	}
	return v.FieldByIndex(idField.Index).String()
}
