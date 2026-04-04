package validate_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/backend/sqlite"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ValidatedDoc struct {
	document.Base
	Name  string `json:"name"  validate:"required,min=3,max=50"`
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age"   validate:"gte=0,lte=130"`
}

type NoTagsDoc struct {
	document.Base
	Title string `json:"title"`
}

type CustomValidatorDoc struct {
	document.Base
	Name string `json:"name" validate:"required"`
}

func (d *CustomValidatorDoc) Validate() error {
	if d.Name == "forbidden" {
		return errors.New("name is forbidden")
	}
	return nil
}

func mustOpenSQLite(t *testing.T, opts ...den.Option) *den.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	backend, err := sqlite.Open(dbPath)
	require.NoError(t, err)

	db, err := den.Open(backend, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestInsertRequiredFieldEmpty(t *testing.T) {
	db := mustOpenSQLite(t, validate.WithValidation())
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{Age: 25}
	err := den.Insert(ctx, db, doc)

	require.Error(t, err)
	assert.ErrorIs(t, err, den.ErrValidation)
}

func TestInsertMinLengthViolation(t *testing.T) {
	db := mustOpenSQLite(t, validate.WithValidation())
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{Name: "ab", Email: "test@example.com", Age: 25}
	err := den.Insert(ctx, db, doc)

	require.ErrorIs(t, err, den.ErrValidation)

	var ve *validate.Errors
	require.ErrorAs(t, err, &ve)
	assert.Len(t, ve.Fields, 1)
	assert.Equal(t, "Name", ve.Fields[0].Field)
	assert.Equal(t, "min", ve.Fields[0].Tag)
}

func TestInsertInvalidEmail(t *testing.T) {
	db := mustOpenSQLite(t, validate.WithValidation())
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{Name: "Alice", Email: "not-an-email", Age: 25}
	err := den.Insert(ctx, db, doc)

	require.Error(t, err)
	assert.ErrorIs(t, err, den.ErrValidation)
}

func TestInsertValidDocument(t *testing.T) {
	db := mustOpenSQLite(t, validate.WithValidation())
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{Name: "Alice", Email: "alice@example.com", Age: 25}
	err := den.Insert(ctx, db, doc)

	require.NoError(t, err)
}

func TestInsertNoValidateTagsNoError(t *testing.T) {
	db := mustOpenSQLite(t, validate.WithValidation())
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, NoTagsDoc{}))

	doc := &NoTagsDoc{Title: ""}
	err := den.Insert(ctx, db, doc)

	require.NoError(t, err)
}

func TestInsertWithoutValidationOption(t *testing.T) {
	db := mustOpenSQLite(t) // no WithValidation
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{} // empty — would fail validation
	err := den.Insert(ctx, db, doc)

	require.NoError(t, err) // but no validator registered
}

func TestUpdateValidationFailure(t *testing.T) {
	db := mustOpenSQLite(t, validate.WithValidation())
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{Name: "Alice", Email: "alice@example.com", Age: 25}
	require.NoError(t, den.Insert(ctx, db, doc))

	doc.Name = "" // violates required
	err := den.Update(ctx, db, doc)

	require.Error(t, err)
	assert.ErrorIs(t, err, den.ErrValidation)
}

func TestBothTagAndInterfaceValidation(t *testing.T) {
	db := mustOpenSQLite(t, validate.WithValidation())
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, CustomValidatorDoc{}))

	// Tag validation fails first (empty name)
	doc := &CustomValidatorDoc{}
	err := den.Insert(ctx, db, doc)
	require.ErrorIs(t, err, den.ErrValidation)

	// Tag passes, interface validation fails
	doc = &CustomValidatorDoc{Name: "forbidden"}
	err = den.Insert(ctx, db, doc)
	require.ErrorIs(t, err, den.ErrValidation)

	// Both pass
	doc = &CustomValidatorDoc{Name: "allowed"}
	err = den.Insert(ctx, db, doc)
	require.NoError(t, err)
}

func TestInsertManyRollsBackOnValidationError(t *testing.T) {
	db := mustOpenSQLite(t, validate.WithValidation())
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	docs := []*ValidatedDoc{
		{Name: "Alice", Email: "alice@example.com", Age: 25},
		{Name: "", Email: "bob@example.com", Age: 30}, // invalid
	}
	err := den.InsertMany(ctx, db, docs)

	require.Error(t, err)
	assert.ErrorIs(t, err, den.ErrValidation)
}

func TestMultipleFieldErrors(t *testing.T) {
	db := mustOpenSQLite(t, validate.WithValidation())
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{Name: "ab", Email: "bad", Age: -1}
	err := den.Insert(ctx, db, doc)

	require.Error(t, err)
	var ve *validate.Errors
	require.ErrorAs(t, err, &ve)
	assert.GreaterOrEqual(t, len(ve.Fields), 3)
}

func TestValidateStructDirectly(t *testing.T) {
	doc := &ValidatedDoc{Name: "ab", Email: "bad", Age: -1}
	err := validate.ValidateStruct(doc)
	require.Error(t, err)

	var ve *validate.Errors
	require.ErrorAs(t, err, &ve)
	assert.GreaterOrEqual(t, len(ve.Fields), 3)
}

func TestValidateStructValid(t *testing.T) {
	doc := &ValidatedDoc{Name: "Alice", Email: "alice@example.com", Age: 25}
	err := validate.ValidateStruct(doc)
	require.NoError(t, err)
}

func TestValidateStructNilReturnsError(t *testing.T) {
	err := validate.ValidateStruct(nil)
	require.Error(t, err)
}

func TestErrorsErrorString(t *testing.T) {
	ve := &validate.Errors{
		Fields: []validate.FieldError{
			{Field: "Name", Tag: "required"},
			{Field: "Email", Tag: "email"},
		},
	}
	s := ve.Error()
	assert.Contains(t, s, "Name")
	assert.Contains(t, s, "Email")
}
