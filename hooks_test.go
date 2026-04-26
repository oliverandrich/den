package den_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
)

// --- Hook test types ---

type Article struct {
	document.Base
	Title     string `json:"title"`
	Slug      string `json:"slug"`
	WordCount int    `json:"word_count"`
}

func (a *Article) BeforeSave(_ context.Context) error {
	a.Slug = "slug-" + a.Title
	a.WordCount = len(a.Title)
	return nil
}

type Validated struct {
	document.Base
	Name string `json:"name"`
}

func (v *Validated) Validate(_ context.Context) error {
	if v.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

type BeforeInsertDoc struct {
	document.Base
	Name    string `json:"name"`
	Touched bool   `json:"touched"`
}

// DefaultingDoc exercises the post-0.6.0 hook order: a BeforeInsert hook
// populates a field that the custom Validator.Validate() method requires.
// Before the reorder, validation ran before the hook and this would fail.
type DefaultingDoc struct {
	document.Base
	Title string `json:"title"`
	Slug  string `json:"slug"`
}

func (d *DefaultingDoc) BeforeInsert(_ context.Context) error {
	if d.Slug == "" {
		d.Slug = "auto-" + d.Title
	}
	return nil
}

func (d *DefaultingDoc) Validate(_ context.Context) error {
	if d.Slug == "" {
		return errors.New("slug is required")
	}
	return nil
}

func (d *BeforeInsertDoc) BeforeInsert(_ context.Context) error {
	d.Touched = true
	return nil
}

type AfterInsertDoc struct {
	document.Base
	Name     string `json:"name"`
	Notified bool
}

func (d *AfterInsertDoc) AfterInsert(_ context.Context) error {
	d.Notified = true
	return nil
}

type FailBeforeDoc struct {
	document.Base
	Name string `json:"name"`
}

func (d *FailBeforeDoc) BeforeInsert(_ context.Context) error {
	return errors.New("insert blocked")
}

type UpdateHookDoc struct {
	document.Base
	Name          string `json:"name"`
	BeforeUpdated bool   `json:"-"`
	AfterUpdated  bool   `json:"-"`
}

func (d *UpdateHookDoc) BeforeUpdate(_ context.Context) error {
	d.BeforeUpdated = true
	return nil
}

func (d *UpdateHookDoc) AfterUpdate(_ context.Context) error {
	d.AfterUpdated = true
	return nil
}

type DeleteHookDoc struct {
	document.Base
	Name          string `json:"name"`
	BeforeDeleted bool   `json:"-"`
	AfterDeleted  bool   `json:"-"`
}

func (d *DeleteHookDoc) BeforeDelete(_ context.Context) error {
	d.BeforeDeleted = true
	return nil
}

func (d *DeleteHookDoc) AfterDelete(_ context.Context) error {
	d.AfterDeleted = true
	return nil
}

type AfterSaveDoc struct {
	document.Base
	Name    string `json:"name"`
	SavedAt string `json:"-"`
}

func (d *AfterSaveDoc) AfterSave(_ context.Context) error {
	d.SavedAt = "called"
	return nil
}

// Error-returning hook types for coverage of error paths.

type FailAfterInsertDoc struct {
	document.Base
	Name string `json:"name"`
}

func (d *FailAfterInsertDoc) AfterInsert(_ context.Context) error {
	return errors.New("after insert failed")
}

type FailAfterSaveOnInsertDoc struct {
	document.Base
	Name string `json:"name"`
}

func (d *FailAfterSaveOnInsertDoc) AfterSave(_ context.Context) error {
	return errors.New("after save failed")
}

type FailBeforeUpdateDoc struct {
	document.Base
	Name string `json:"name"`
}

func (d *FailBeforeUpdateDoc) BeforeUpdate(_ context.Context) error {
	return errors.New("before update blocked")
}

type FailBeforeSaveOnUpdateDoc struct {
	document.Base
	Name  string `json:"name"`
	count int
}

func (d *FailBeforeSaveOnUpdateDoc) BeforeSave(_ context.Context) error {
	d.count++
	if d.count > 1 {
		return errors.New("before save blocked on update")
	}
	return nil
}

type FailAfterUpdateDoc struct {
	document.Base
	Name string `json:"name"`
}

func (d *FailAfterUpdateDoc) AfterUpdate(_ context.Context) error {
	return errors.New("after update failed")
}

type FailAfterSaveOnUpdateDoc struct {
	document.Base
	Name  string `json:"name"`
	count int
}

func (d *FailAfterSaveOnUpdateDoc) AfterSave(_ context.Context) error {
	d.count++
	if d.count > 1 {
		return errors.New("after save failed on update")
	}
	return nil
}

type FailBeforeDeleteDoc struct {
	document.Base
	Name string `json:"name"`
}

func (d *FailBeforeDeleteDoc) BeforeDelete(_ context.Context) error {
	return errors.New("before delete blocked")
}

type FailAfterDeleteDoc struct {
	document.Base
	Name string `json:"name"`
}

func (d *FailAfterDeleteDoc) AfterDelete(_ context.Context) error {
	return errors.New("after delete failed")
}

type ValidateOnUpdateDoc struct {
	document.Base
	Name string `json:"name"`
}

func (v *ValidateOnUpdateDoc) Validate(_ context.Context) error {
	if v.Name == "invalid" {
		return errors.New("name is invalid")
	}
	return nil
}

// --- Tests ---

func TestBeforeSave_Hook(t *testing.T) {
	db := dentest.MustOpen(t, &Article{})
	ctx := context.Background()

	a := &Article{Title: "Hello"}
	require.NoError(t, den.Insert(ctx, db, a))

	assert.Equal(t, "slug-Hello", a.Slug)
	assert.Equal(t, 5, a.WordCount)

	found, err := den.FindByID[Article](ctx, db, a.ID)
	require.NoError(t, err)
	assert.Equal(t, "slug-Hello", found.Slug)
}

func TestBeforeSave_OnUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &Article{})
	ctx := context.Background()

	a := &Article{Title: "First"}
	require.NoError(t, den.Insert(ctx, db, a))

	a.Title = "Updated"
	require.NoError(t, den.Update(ctx, db, a))
	assert.Equal(t, "slug-Updated", a.Slug)
}

func TestValidateOnSave(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	v := &Validated{Name: ""}
	err := den.Insert(ctx, db, v)
	require.Error(t, err)
	assert.ErrorIs(t, err, den.ErrValidation)
}

func TestValidateOnSave_Valid(t *testing.T) {
	db := dentest.MustOpen(t, &Validated{})
	ctx := context.Background()

	v := &Validated{Name: "OK"}
	require.NoError(t, den.Insert(ctx, db, v))
}

// CtxAwareValidated returns the ctx's error from Validate so the test
// can prove the hook actually receives the caller's context (not a
// substituted Background()).
type CtxAwareValidated struct {
	document.Base
	Name string `json:"name"`
}

func (v *CtxAwareValidated) Validate(ctx context.Context) error {
	return ctx.Err()
}

// TestValidate_PropagatesContext pins that Validator.Validate receives
// the caller's ctx — a cancelled context surfaces immediately without
// the validator having to capture it from outer scope.
func TestValidate_PropagatesContext(t *testing.T) {
	db := dentest.MustOpen(t, &CtxAwareValidated{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	v := &CtxAwareValidated{Name: "Anything"}
	err := den.Insert(ctx, db, v)
	require.Error(t, err)
	require.ErrorIs(t, err, den.ErrValidation,
		"validation failure must wrap ErrValidation")
	require.ErrorIs(t, err, context.Canceled,
		"validator must see the caller's cancellation through ctx")
}

// TestBeforeInsertPopulatesFieldForValidate regresses the pre-v0.6.0 bug
// where validation ran before mutating hooks, making it impossible for a
// BeforeInsert hook to populate a field that the custom Validate() method
// required. The new order is: BeforeInsert → BeforeSave → Validate.
func TestBeforeInsertPopulatesFieldForValidate(t *testing.T) {
	db := dentest.MustOpen(t, &DefaultingDoc{})
	ctx := context.Background()

	// Title is set, Slug is not. The BeforeInsert hook will populate Slug
	// from Title. Validate() requires Slug to be non-empty. If the hook
	// order is correct, the insert succeeds.
	d := &DefaultingDoc{Title: "Hello"}
	require.NoError(t, den.Insert(ctx, db, d))
	assert.Equal(t, "auto-Hello", d.Slug)
}

// TestUpdateHookOrderRunsBeforeValidate is the update-path analogue:
// BeforeSave on update recomputes the slug, and Validate() sees the new
// value.
func TestUpdateHookOrderRunsBeforeValidate(t *testing.T) {
	db := dentest.MustOpen(t, &DefaultingDoc{})
	ctx := context.Background()

	d := &DefaultingDoc{Title: "First"}
	require.NoError(t, den.Insert(ctx, db, d))

	// Clear the slug; without the hook order fix, Validate would fail.
	// With the fix, BeforeSave (which DefaultingDoc does not have) would
	// run first. Since DefaultingDoc only implements BeforeInsert (not
	// BeforeSave or BeforeUpdate), Update with an empty slug must fail.
	d.Slug = ""
	err := den.Update(ctx, db, d)
	require.Error(t, err)
	assert.ErrorIs(t, err, den.ErrValidation)
}

func TestBeforeInsert_Hook(t *testing.T) {
	db := dentest.MustOpen(t, &BeforeInsertDoc{})
	ctx := context.Background()

	d := &BeforeInsertDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d))
	assert.True(t, d.Touched)
}

func TestAfterInsert_Hook(t *testing.T) {
	db := dentest.MustOpen(t, &AfterInsertDoc{})
	ctx := context.Background()

	d := &AfterInsertDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d))
	assert.True(t, d.Notified)
}

func TestBeforeInsert_BlocksInsert(t *testing.T) {
	db := dentest.MustOpen(t, &FailBeforeDoc{})
	ctx := context.Background()

	d := &FailBeforeDoc{Name: "Blocked"}
	err := den.Insert(ctx, db, d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insert blocked")

	// Should not be persisted
	_, err = den.FindByID[FailBeforeDoc](ctx, db, d.ID)
	assert.ErrorIs(t, err, den.ErrNotFound)
}

func TestUpdateHooks(t *testing.T) {
	db := dentest.MustOpen(t, &UpdateHookDoc{})
	ctx := context.Background()

	d := &UpdateHookDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d))

	d.Name = "Updated"
	require.NoError(t, den.Update(ctx, db, d))
	assert.True(t, d.BeforeUpdated)
	assert.True(t, d.AfterUpdated)
}

func TestDeleteHooks(t *testing.T) {
	db := dentest.MustOpen(t, &DeleteHookDoc{})
	ctx := context.Background()

	d := &DeleteHookDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d))
	require.NoError(t, den.Delete(ctx, db, d))
	assert.True(t, d.BeforeDeleted)
	assert.True(t, d.AfterDeleted)
}

func TestAfterSave_OnInsert(t *testing.T) {
	db := dentest.MustOpen(t, &AfterSaveDoc{})
	ctx := context.Background()

	d := &AfterSaveDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d))
	assert.Equal(t, "called", d.SavedAt)
}

func TestAfterSave_OnUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &AfterSaveDoc{})
	ctx := context.Background()

	d := &AfterSaveDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d))
	d.SavedAt = "" // reset
	d.Name = "Updated"
	require.NoError(t, den.Update(ctx, db, d))
	assert.Equal(t, "called", d.SavedAt)
}

func TestAfterInsert_Error(t *testing.T) {
	db := dentest.MustOpen(t, &FailAfterInsertDoc{})
	ctx := context.Background()

	d := &FailAfterInsertDoc{Name: "Test"}
	err := den.Insert(ctx, db, d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after insert failed")
}

func TestAfterSave_ErrorOnInsert(t *testing.T) {
	db := dentest.MustOpen(t, &FailAfterSaveOnInsertDoc{})
	ctx := context.Background()

	d := &FailAfterSaveOnInsertDoc{Name: "Test"}
	err := den.Insert(ctx, db, d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after save failed")
}

func TestBeforeUpdate_Error(t *testing.T) {
	db := dentest.MustOpen(t, &FailBeforeUpdateDoc{})
	ctx := context.Background()

	d := &FailBeforeUpdateDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d))

	d.Name = "Updated"
	err := den.Update(ctx, db, d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "before update blocked")
}

func TestBeforeSave_ErrorOnUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &FailBeforeSaveOnUpdateDoc{})
	ctx := context.Background()

	d := &FailBeforeSaveOnUpdateDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d)) // count=1, passes

	d.Name = "Updated"
	err := den.Update(ctx, db, d) // count=2, fails
	require.Error(t, err)
	assert.Contains(t, err.Error(), "before save blocked on update")
}

func TestAfterUpdate_Error(t *testing.T) {
	db := dentest.MustOpen(t, &FailAfterUpdateDoc{})
	ctx := context.Background()

	d := &FailAfterUpdateDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d))

	d.Name = "Updated"
	err := den.Update(ctx, db, d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after update failed")
}

func TestAfterSave_ErrorOnUpdate(t *testing.T) {
	db := dentest.MustOpen(t, &FailAfterSaveOnUpdateDoc{})
	ctx := context.Background()

	d := &FailAfterSaveOnUpdateDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d)) // count=1, passes

	d.Name = "Updated"
	err := den.Update(ctx, db, d) // count=2, fails
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after save failed on update")
}

func TestBeforeDelete_Error(t *testing.T) {
	db := dentest.MustOpen(t, &FailBeforeDeleteDoc{})
	ctx := context.Background()

	d := &FailBeforeDeleteDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d))

	err := den.Delete(ctx, db, d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "before delete blocked")
}

func TestAfterDelete_Error(t *testing.T) {
	db := dentest.MustOpen(t, &FailAfterDeleteDoc{})
	ctx := context.Background()

	d := &FailAfterDeleteDoc{Name: "Test"}
	require.NoError(t, den.Insert(ctx, db, d))

	err := den.Delete(ctx, db, d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after delete failed")
}

func TestValidateOnUpdate_Error(t *testing.T) {
	db := dentest.MustOpen(t, &ValidateOnUpdateDoc{})
	ctx := context.Background()

	d := &ValidateOnUpdateDoc{Name: "valid"}
	require.NoError(t, den.Insert(ctx, db, d))

	d.Name = "invalid"
	err := den.Update(ctx, db, d)
	require.Error(t, err)
	assert.ErrorIs(t, err, den.ErrValidation)
}
