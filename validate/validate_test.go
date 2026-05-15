package validate_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/oliverandrich/den"
	_ "github.com/oliverandrich/den/backend/sqlite"
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

func (d *CustomValidatorDoc) Validate(_ context.Context) error {
	if d.Name == "forbidden" {
		return errors.New("name is forbidden")
	}
	return nil
}

// DefaultingTagDoc exercises the post-v0.6.0 hook order with tag-based
// validation: a BeforeInsert hook populates a field that a validate tag
// marks as required. Before the reorder, tag validation ran before the
// hook and this would fail.
type DefaultingTagDoc struct {
	document.Base
	Name string `json:"name"`
	Slug string `json:"slug" validate:"required"`
}

func (d *DefaultingTagDoc) BeforeInsert(_ context.Context) error {
	if d.Slug == "" {
		d.Slug = "auto-" + d.Name
	}
	return nil
}

func mustOpenSQLite(t *testing.T) *den.DB {
	t.Helper()
	dsn := "sqlite:///" + filepath.Join(t.TempDir(), "test.db")
	db, err := den.OpenURL(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestInsertRequiredFieldEmpty(t *testing.T) {
	db := mustOpenSQLite(t)
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{Age: 25}
	err := den.Save(ctx, db, doc)

	require.Error(t, err)
	assert.ErrorIs(t, err, den.ErrValidation)
}

func TestInsertMinLengthViolation(t *testing.T) {
	db := mustOpenSQLite(t)
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{Name: "ab", Email: "test@example.com", Age: 25}
	err := den.Save(ctx, db, doc)

	require.ErrorIs(t, err, den.ErrValidation)

	var ve *validate.Errors
	require.ErrorAs(t, err, &ve)
	assert.Len(t, ve.Fields, 1)
	assert.Equal(t, "Name", ve.Fields[0].Field)
	assert.Equal(t, "min", ve.Fields[0].Tag)
}

func TestInsertInvalidEmail(t *testing.T) {
	db := mustOpenSQLite(t)
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{Name: "Alice", Email: "not-an-email", Age: 25}
	err := den.Save(ctx, db, doc)

	require.Error(t, err)
	assert.ErrorIs(t, err, den.ErrValidation)
}

func TestInsertValidDocument(t *testing.T) {
	db := mustOpenSQLite(t)
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{Name: "Alice", Email: "alice@example.com", Age: 25}
	err := den.Save(ctx, db, doc)

	require.NoError(t, err)
}

func TestInsertNoValidateTagsNoError(t *testing.T) {
	db := mustOpenSQLite(t)
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, NoTagsDoc{}))

	doc := &NoTagsDoc{Title: ""}
	err := den.Save(ctx, db, doc)

	require.NoError(t, err)
}

// TestTagConstraintsAreMandatory pins that struct-tag constraints cannot
// be bypassed — there is no opt-in option, and a doc with `validate:`
// tags always has those constraints enforced by Den on every write.
func TestTagConstraintsAreMandatory(t *testing.T) {
	db := mustOpenSQLite(t)
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{} // empty Name + Email — would violate required
	err := den.Save(ctx, db, doc)

	require.Error(t, err, "Den must enforce validate-tag constraints unconditionally")
	require.ErrorIs(t, err, den.ErrValidation)
}

func TestUpdateValidationFailure(t *testing.T) {
	db := mustOpenSQLite(t)
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{Name: "Alice", Email: "alice@example.com", Age: 25}
	require.NoError(t, den.Save(ctx, db, doc))

	doc.Name = "" // violates required
	err := den.Save(ctx, db, doc)

	require.Error(t, err)
	assert.ErrorIs(t, err, den.ErrValidation)
}

func TestBothTagAndInterfaceValidation(t *testing.T) {
	db := mustOpenSQLite(t)
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, CustomValidatorDoc{}))

	// Tag validation fails first (empty name)
	doc := &CustomValidatorDoc{}
	err := den.Save(ctx, db, doc)
	require.ErrorIs(t, err, den.ErrValidation)

	// Tag passes, interface validation fails
	doc = &CustomValidatorDoc{Name: "forbidden"}
	err = den.Save(ctx, db, doc)
	require.ErrorIs(t, err, den.ErrValidation)

	// Both pass
	doc = &CustomValidatorDoc{Name: "allowed"}
	err = den.Save(ctx, db, doc)
	require.NoError(t, err)
}

// TestBeforeInsertPopulatesRequiredTagField regresses the pre-v0.6.0 order
// bug: tag validation must run AFTER BeforeInsert/BeforeSave hooks so that
// a hook can populate a default value for a field marked as required.
func TestBeforeInsertPopulatesRequiredTagField(t *testing.T) {
	db := mustOpenSQLite(t)
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, DefaultingTagDoc{}))

	// Slug is required, but the BeforeInsert hook populates it from Name.
	doc := &DefaultingTagDoc{Name: "Hello"}
	err := den.Save(ctx, db, doc)
	require.NoError(t, err)
	assert.Equal(t, "auto-Hello", doc.Slug)
}

func TestInsertManyRollsBackOnValidationError(t *testing.T) {
	db := mustOpenSQLite(t)
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	docs := []*ValidatedDoc{
		{Name: "Alice", Email: "alice@example.com", Age: 25},
		{Name: "", Email: "bob@example.com", Age: 30}, // invalid
	}
	err := den.SaveAll(ctx, db, docs)

	require.Error(t, err)
	assert.ErrorIs(t, err, den.ErrValidation)
}

func TestMultipleFieldErrors(t *testing.T) {
	db := mustOpenSQLite(t)
	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, ValidatedDoc{}))

	doc := &ValidatedDoc{Name: "ab", Email: "bad", Age: -1}
	err := den.Save(ctx, db, doc)

	require.Error(t, err)
	var ve *validate.Errors
	require.ErrorAs(t, err, &ve)
	assert.GreaterOrEqual(t, len(ve.Fields), 3)
}

func TestValidateStructDirectly(t *testing.T) {
	doc := &ValidatedDoc{Name: "ab", Email: "bad", Age: -1}
	err := validate.Document(doc)
	require.Error(t, err)

	var ve *validate.Errors
	require.ErrorAs(t, err, &ve)
	assert.GreaterOrEqual(t, len(ve.Fields), 3)
}

func TestValidateStructValid(t *testing.T) {
	doc := &ValidatedDoc{Name: "Alice", Email: "alice@example.com", Age: 25}
	err := validate.Document(doc)
	require.NoError(t, err)
}

func TestValidateStructNilReturnsError(t *testing.T) {
	err := validate.Document(nil)
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
