// SPDX-License-Identifier: MIT

package search_test

import (
	"testing"

	"github.com/oliverandrich/den/search"
	"github.com/stretchr/testify/assert"
)

func TestLiteralFTS5(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single token", "foo", `"foo"`},
		{"two tokens AND-joined", "foo bar", `"foo" "bar"`},
		{"empty", "", ""},
		{"all blank", "   ", ""},
		{"tabs and newlines", "\t\n", ""},
		{"embedded quote doubled", `foo"bar`, `"foo""bar"`},
		{"whitespace runs collapse", "  a  b  ", `"a" "b"`},
		{"operators are quoted, not interpreted", "red OR yellow", `"red" "OR" "yellow"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, search.LiteralFTS5(tc.in))
		})
	}
}
