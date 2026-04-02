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

func (v *Validated) Validate() error {
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
