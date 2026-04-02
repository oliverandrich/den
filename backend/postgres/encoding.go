package postgres

import (
	json "github.com/goccy/go-json"

	"github.com/oliverandrich/den"
)

// jsonEncoder uses standard encoding/json with json struct tags.
type jsonEncoder struct{}

func newJSONEncoder() den.Encoder {
	return &jsonEncoder{}
}

func (e *jsonEncoder) Encode(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (e *jsonEncoder) Decode(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
