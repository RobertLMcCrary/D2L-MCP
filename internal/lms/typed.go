package lms

import (
	"context"
	"encoding/json"
)

// TypedResponse provides a typed projection, exact payload, and pagination
// metadata through any provider. It is a domain helper, not a wire schema.
type TypedResponse[T any] struct {
	Document[T]
	Pages        int
	Next         *Request
	PageMetadata []json.RawMessage
}

func Fetch[T any](ctx context.Context, provider Provider, request Request) (TypedResponse[T], error) {
	response, err := provider.Do(ctx, request)
	if err != nil {
		return TypedResponse[T]{}, err
	}
	doc, err := Decode[T](response.Data)
	if err != nil {
		return TypedResponse[T]{}, err
	}
	return TypedResponse[T]{Document: doc, Pages: response.Pages, Next: response.Next, PageMetadata: response.PageMetadata}, nil
}
