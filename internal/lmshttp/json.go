// Package lmshttp holds protocol primitives shared by LMS adapters.
package lmshttp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
)

func ReadJSON(r io.Reader, limit int64) (json.RawMessage, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("JSON response exceeds %d bytes", limit)
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return json.RawMessage("null"), nil
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("invalid JSON response")
	}
	return b, nil
}

func CloneQuery(values url.Values) url.Values {
	out := url.Values{}
	for k, v := range values {
		out[k] = append([]string(nil), v...)
	}
	return out
}
