package postgres

import (
	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/internal"
)

func newJSONEncoder() den.Encoder {
	return &internal.Encoder{}
}
