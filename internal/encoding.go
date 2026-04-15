package internal

import (
	json "github.com/goccy/go-json"
)

// Encoder provides JSON encoding/decoding for document storage.
type Encoder struct{}

func (e *Encoder) Encode(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (e *Encoder) Decode(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
