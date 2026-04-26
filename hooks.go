package den

import (
	"context"
	"fmt"
)

// Lifecycle hook interfaces. Implement on document structs.

type BeforeInserter interface {
	BeforeInsert(ctx context.Context) error
}

type AfterInserter interface {
	AfterInsert(ctx context.Context) error
}

type BeforeUpdater interface {
	BeforeUpdate(ctx context.Context) error
}

type AfterUpdater interface {
	AfterUpdate(ctx context.Context) error
}

// BeforeDeleter fires before any deletion — both soft and hard. The hook
// runs before the soft-delete flip OR the physical row removal, whichever
// the call resolves to. Use BeforeSoftDeleter for soft-only logic.
//
// Ordering on the soft path: BeforeDelete → BeforeSoftDelete → [write]
// → AfterSoftDelete → AfterDelete.
//
// Ordering on the hard path (HardDelete() option, or no SoftDelete embed):
// BeforeDelete → [write] → AfterDelete; the soft-only hooks are skipped.
type BeforeDeleter interface {
	BeforeDelete(ctx context.Context) error
}

// AfterDeleter fires after any deletion completes — both soft and hard.
// See BeforeDeleter for the full hook ordering on each path.
type AfterDeleter interface {
	AfterDelete(ctx context.Context) error
}

// BeforeSoftDeleter fires only on the soft-delete path — after BeforeDelete,
// before the write. HardDelete() bypasses this hook. Use it for audit-log
// side effects that should not fire on permanent deletion.
//
// Full ordering: BeforeDelete → BeforeSoftDelete → [write] →
// AfterSoftDelete → AfterDelete.
type BeforeSoftDeleter interface {
	BeforeSoftDelete(ctx context.Context) error
}

// AfterSoftDeleter fires only on the soft-delete path — after the write,
// before AfterDelete. HardDelete() bypasses this hook. See BeforeDeleter
// for the full hook ordering.
type AfterSoftDeleter interface {
	AfterSoftDelete(ctx context.Context) error
}

type BeforeSaver interface {
	BeforeSave(ctx context.Context) error
}

type AfterSaver interface {
	AfterSave(ctx context.Context) error
}

// Validator is the custom-validation hook. Implement it on a document
// to enforce invariants beyond what struct tag validation can express.
// Returning an error rolls back the surrounding Insert / Update without
// touching storage.
//
// The passed ctx is the same one threaded through the surrounding
// Insert / Update call — use it for cancellation, deadlines, DB lookups
// inside the validator, outbound HTTP calls that need to participate
// in the request, or tracing spans. Matches the signature of every
// other Den hook.
type Validator interface {
	Validate(ctx context.Context) error
}

// runBeforeInsertHooks runs the mutating before-hooks for insert in order:
// BeforeInsert first, then BeforeSave. Validation runs separately via
// runValidationHooks after these have populated any defaults or computed
// fields, so the validator sees the final document that will be written.
func runBeforeInsertHooks(ctx context.Context, doc any) error {
	if h, ok := doc.(BeforeInserter); ok {
		if err := h.BeforeInsert(ctx); err != nil {
			return err
		}
	}
	if h, ok := doc.(BeforeSaver); ok {
		if err := h.BeforeSave(ctx); err != nil {
			return err
		}
	}
	return nil
}

// runValidationHooks runs the custom Validator.Validate(ctx) method,
// if any. Runs after BeforeInsert/BeforeUpdate/BeforeSave so the
// validator sees the final, fully-populated document. Tag-based
// validation (via DB.tagValidator) is invoked separately in the CRUD
// functions so both declarative and custom validation see the same
// post-hook state.
func runValidationHooks(ctx context.Context, doc any) error {
	if v, ok := doc.(Validator); ok {
		if err := v.Validate(ctx); err != nil {
			return fmt.Errorf("%w: %w", ErrValidation, err)
		}
	}
	return nil
}

func runAfterInsertHooks(ctx context.Context, doc any) error {
	if h, ok := doc.(AfterInserter); ok {
		if err := h.AfterInsert(ctx); err != nil {
			return err
		}
	}
	if h, ok := doc.(AfterSaver); ok {
		if err := h.AfterSave(ctx); err != nil {
			return err
		}
	}
	return nil
}

// runBeforeUpdateHooks runs the mutating before-hooks for update in order:
// BeforeUpdate first, then BeforeSave. Validation runs separately via
// runValidationHooks after these have populated any computed fields.
func runBeforeUpdateHooks(ctx context.Context, doc any) error {
	if h, ok := doc.(BeforeUpdater); ok {
		if err := h.BeforeUpdate(ctx); err != nil {
			return err
		}
	}
	if h, ok := doc.(BeforeSaver); ok {
		if err := h.BeforeSave(ctx); err != nil {
			return err
		}
	}
	return nil
}

func runAfterUpdateHooks(ctx context.Context, doc any) error {
	if h, ok := doc.(AfterUpdater); ok {
		if err := h.AfterUpdate(ctx); err != nil {
			return err
		}
	}
	if h, ok := doc.(AfterSaver); ok {
		if err := h.AfterSave(ctx); err != nil {
			return err
		}
	}
	return nil
}

func runBeforeDeleteHooks(ctx context.Context, doc any) error {
	if h, ok := doc.(BeforeDeleter); ok {
		if err := h.BeforeDelete(ctx); err != nil {
			return err
		}
	}
	return nil
}

func runAfterDeleteHooks(ctx context.Context, doc any) error {
	if h, ok := doc.(AfterDeleter); ok {
		if err := h.AfterDelete(ctx); err != nil {
			return err
		}
	}
	return nil
}

func runBeforeSoftDeleteHooks(ctx context.Context, doc any) error {
	if h, ok := doc.(BeforeSoftDeleter); ok {
		if err := h.BeforeSoftDelete(ctx); err != nil {
			return err
		}
	}
	return nil
}

func runAfterSoftDeleteHooks(ctx context.Context, doc any) error {
	if h, ok := doc.(AfterSoftDeleter); ok {
		if err := h.AfterSoftDelete(ctx); err != nil {
			return err
		}
	}
	return nil
}
