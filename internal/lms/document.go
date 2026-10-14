package lms

import "encoding/json"

// Document pairs a useful typed view with the exact payload. Typed projections
// may evolve without discarding unknown provider fields or precision in IDs.
type Document[T any] struct {
	Value T
	Raw   json.RawMessage
}

func Decode[T any](raw json.RawMessage) (Document[T], error) {
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return Document[T]{}, err
	}
	return Document[T]{Value: value, Raw: append(json.RawMessage(nil), raw...)}, nil
}

func (d Document[T]) MarshalJSON() ([]byte, error) {
	if len(d.Raw) != 0 {
		return d.Raw, nil
	}
	return json.Marshal(d.Value)
}

func (d *Document[T]) UnmarshalJSON(raw []byte) error {
	decoded, err := Decode[T](raw)
	if err != nil {
		return err
	}
	*d = decoded
	return nil
}
