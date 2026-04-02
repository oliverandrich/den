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

type BeforeDeleter interface {
	BeforeDelete(ctx context.Context) error
}

type AfterDeleter interface {
	AfterDelete(ctx context.Context) error
}

type BeforeSaver interface {
	BeforeSave(ctx context.Context) error
}

type AfterSaver interface {
	AfterSave(ctx context.Context) error
}

type Validator interface {
	Validate() error
}

// runInsertHooks runs validation and before-hooks for insert.
// Returns an error if any hook fails (caller should abort the insert).
func runBeforeInsertHooks(ctx context.Context, doc any) error {
	if v, ok := doc.(Validator); ok {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrValidation, err)
		}
	}
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

func runBeforeUpdateHooks(ctx context.Context, doc any) error {
	if v, ok := doc.(Validator); ok {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrValidation, err)
		}
	}
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
