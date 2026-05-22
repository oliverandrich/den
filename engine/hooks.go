package engine

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

// runHook invokes call on doc when doc implements I. Returns whatever
// the hook method returned, or nil if doc doesn't implement I. Callers
// typically pass an interface method expression like
// BeforeInserter.BeforeInsert as call.
func runHook[I any](ctx context.Context, doc any, call func(I, context.Context) error) error {
	if h, ok := doc.(I); ok {
		return call(h, ctx)
	}
	return nil
}

// runBeforeInsertHooks fires BeforeInsert then BeforeSave. Validation runs
// separately via runValidationHooks after these have populated any defaults
// or computed fields, so the validator sees the final document.
func runBeforeInsertHooks(ctx context.Context, doc any) error {
	if err := runHook(ctx, doc, BeforeInserter.BeforeInsert); err != nil {
		return err
	}
	return runHook(ctx, doc, BeforeSaver.BeforeSave)
}

// runValidationHooks runs the custom Validator.Validate(ctx) method, if
// any, and wraps any returned error with ErrValidation so callers can
// errors.Is for it. Runs after BeforeInsert/BeforeUpdate/BeforeSave so the
// validator sees the final, fully-populated document. Stays inline rather
// than using runHook because of the error-wrap requirement.
func runValidationHooks(ctx context.Context, doc any) error {
	if v, ok := doc.(Validator); ok {
		if err := v.Validate(ctx); err != nil {
			return fmt.Errorf("%w: %w", ErrValidation, err)
		}
	}
	return nil
}

func runAfterInsertHooks(ctx context.Context, doc any) error {
	if err := runHook(ctx, doc, AfterInserter.AfterInsert); err != nil {
		return err
	}
	return runHook(ctx, doc, AfterSaver.AfterSave)
}

// runBeforeUpdateHooks fires BeforeUpdate then BeforeSave. Validation runs
// separately via runValidationHooks after these have populated any computed
// fields.
func runBeforeUpdateHooks(ctx context.Context, doc any) error {
	if err := runHook(ctx, doc, BeforeUpdater.BeforeUpdate); err != nil {
		return err
	}
	return runHook(ctx, doc, BeforeSaver.BeforeSave)
}

func runAfterUpdateHooks(ctx context.Context, doc any) error {
	if err := runHook(ctx, doc, AfterUpdater.AfterUpdate); err != nil {
		return err
	}
	return runHook(ctx, doc, AfterSaver.AfterSave)
}

func runBeforeDeleteHooks(ctx context.Context, doc any) error {
	return runHook(ctx, doc, BeforeDeleter.BeforeDelete)
}

func runAfterDeleteHooks(ctx context.Context, doc any) error {
	return runHook(ctx, doc, AfterDeleter.AfterDelete)
}

func runBeforeSoftDeleteHooks(ctx context.Context, doc any) error {
	return runHook(ctx, doc, BeforeSoftDeleter.BeforeSoftDelete)
}

func runAfterSoftDeleteHooks(ctx context.Context, doc any) error {
	return runHook(ctx, doc, AfterSoftDeleter.AfterSoftDelete)
}
