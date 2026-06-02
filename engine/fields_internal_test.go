package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/oliverandrich/den/document"
	"github.com/stretchr/testify/assert"
)

// TestServerOwnedFieldNames_CoversBaseEmbeds guards the canonical list against
// drift: every underscore-prefixed JSON field that document.Base and
// document.SoftDelete declare is server-owned and must be in
// serverOwnedFieldNames so Replace / PreserveServerFields preserve it. Adding
// a server-owned field to those embeds without listing it here fails this test
// — the whole point of den-cczd is that this set lives in one checked place.
func TestServerOwnedFieldNames_CoversBaseEmbeds(t *testing.T) {
	for _, embed := range []reflect.Type{
		reflect.TypeFor[document.Base](),
		reflect.TypeFor[document.SoftDelete](),
	} {
		for i := range embed.NumField() {
			f := embed.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if !strings.HasPrefix(name, "_") {
				continue
			}
			assert.Contains(t, serverOwnedFieldNames, name,
				"%s.%s (%q) is server-owned but missing from serverOwnedFieldNames",
				embed.Name(), f.Name, name)
		}
	}
}
