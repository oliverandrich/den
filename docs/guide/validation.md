# Validation

Den supports two validation mechanisms that can be used independently or combined.

## Validator Interface

Implement the `Validate() error` method on your document struct for custom business logic validation:

```go
type Article struct {
    document.Base
    Title string `json:"title"`
    Body  string `json:"body"`
}

func (a *Article) Validate() error {
    if a.Title == "" {
        return errors.New("title is required")
    }
    if len(a.Body) < 10 {
        return errors.New("body must be at least 10 characters")
    }
    return nil
}
```

The `Validate()` hook is called automatically before every `Insert` and `Update`. If it returns an error, the write is aborted.

## Struct Tag Validation

For structural validation rules, Den integrates with [go-playground/validator](https://github.com/go-playground/validator) via the `validate` package. Enable it with `validate.WithValidation()` when opening the database:

```go
import "github.com/oliverandrich/den/validate"

db, err := den.OpenURL("sqlite:///data.db", validate.WithValidation())
```

Then add `validate` tags to your struct fields:

```go
type User struct {
    document.Base
    Username string `json:"username" den:"unique" validate:"required,min=3,max=50"`
    Email    string `json:"email"    den:"unique" validate:"required,email"`
    Age      int    `json:"age"                   validate:"gte=0,lte=150"`
    Website  string `json:"website"               validate:"omitempty,url"`
}
```

!!! note
    Without `validate.WithValidation()`, only the `Validate()` hook runs. Tag validation is opt-in for backward compatibility.

## Execution Order

Both validation mechanisms run **after** any mutating `BeforeInsert` / `BeforeUpdate` / `BeforeSave` hook, so hooks can populate default values, compute derived fields, or normalize inputs before the constraints are checked.

The full order during `Insert` and `Update`:

1. `BeforeInsert` / `BeforeUpdate` hook (mutating)
2. `BeforeSave` hook (mutating, runs on both insert and update)
3. Struct tag validation (`validate` tags)
4. `Validate()` hook (custom business logic)
5. Write to the database

If tag validation fails, the `Validate()` hook is not called. Either step aborts the write and rolls back the transaction.

!!! note
    This is the same pattern used by ActiveRecord, Django ORM, and SQLAlchemy: hooks first, then validation against the final state. It lets you write things like "`BeforeInsert` generates the slug from the title; tag validation then requires the slug to be non-empty" without fighting the framework.

## Error Handling

Validation errors are wrapped with `den.ErrValidation`:

```go
err := den.Insert(ctx, db, &user)
if errors.Is(err, den.ErrValidation) {
    fmt.Println("Validation failed:", err)
}
```

For field-level details from tag validation, use `errors.As`:

```go
var validationErr *validate.Errors
if errors.As(err, &validationErr) {
    for _, fe := range validationErr.Fields {
        fmt.Printf("Field %s failed on %s\n", fe.Field, fe.Tag)
    }
}
```

## Full Example

```go
type User struct {
    document.Base
    Username string `json:"username" den:"unique" validate:"required,min=3"`
    Email    string `json:"email"    den:"unique" validate:"required,email"`
    Bio      string `json:"bio"                   validate:"max=500"`
}

func (u *User) Validate() error {
    if strings.Contains(u.Username, " ") {
        return errors.New("username must not contain spaces")
    }
    return nil
}
```

```go
db, _ := den.OpenURL("sqlite:///data.db", validate.WithValidation())
den.Register(ctx, db, &User{})

user := &User{Username: "ab", Email: "invalid"}
err := den.Insert(ctx, db, user)
// err wraps den.ErrValidation — "ab" is too short, "invalid" is not a valid email
```

!!! tip
    Use `validate` tags for structural constraints (required, format, length) and the `Validate()` hook for business rules that depend on multiple fields or external state.
