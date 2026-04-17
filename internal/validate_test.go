package internal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateFieldName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple lowercase", "name", false},
		{"with digits", "field1", false},
		{"leading underscore", "_id", false},
		{"double underscore", "__internal", false},
		{"snake case", "user_name", false},
		{"mixed case", "userName", false},
		{"uppercase only", "NAME", false},

		{"empty", "", true},
		{"leading digit", "1field", true},
		{"dot", "a.b", true},
		{"space", "a b", true},
		{"single quote", "a'b", true},
		{"double quote", "a\"b", true},
		{"semicolon", "a;b", true},
		{"injection attempt", "name';DROP TABLE x;--", true},
		{"dash", "a-b", true},
		{"unicode", "fööbar", true},
		{"backslash", "a\\b", true},
		{"tab", "a\tb", true},
		{"newline", "a\nb", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFieldName(tt.input)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidFieldName)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateFieldName_ErrorIsSentinel(t *testing.T) {
	err := ValidateFieldName("bad;name")
	require.ErrorIs(t, err, ErrInvalidFieldName)
}
