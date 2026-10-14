// Package lms defines the transport-independent contract between MCP and LMS adapters.
// Provider-specific payloads are preserved: forcing every LMS into a lowest-common-
// denominator model loses permissions, extensions, and newly introduced fields.
package lms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type Request struct {
	Method     string          `json:"method,omitempty"`
	Path       string          `json:"path"`
	Query      url.Values      `json:"query,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
	Paginate   bool            `json:"paginate,omitempty"`
	Collection string          `json:"collection,omitempty"`
}

type Response struct {
	Data         json.RawMessage   `json:"data"`
	Pages        int               `json:"pages"`
	Next         *Request          `json:"next,omitempty"`
	PageMetadata []json.RawMessage `json:"page_metadata,omitempty"`
}

type Capabilities struct {
	Provider    string   `json:"provider"`
	Read        bool     `json:"read"`
	Write       bool     `json:"write"`
	Upload      bool     `json:"upload"`
	Download    bool     `json:"download"`
	APIPrefixes []string `json:"api_prefixes"`
}

type Provider interface {
	Capabilities() Capabilities
	Do(context.Context, Request) (Response, error)
}

type ErrorCode string

type Error struct {
	Code       ErrorCode `json:"code"`
	Message    string    `json:"message"`
	StatusCode int       `json:"status_code,omitempty"`
	NextStep   string    `json:"next_step,omitempty"`
	Details    []string  `json:"details,omitempty"`
	Cause      error     `json:"-"`
}

func (e *Error) Error() string {
	message := e.Message
	if len(e.Details) > 0 {
		message += " (" + strings.Join(e.Details, "; ") + ")"
	}
	if e.NextStep == "" {
		return message
	}
	return fmt.Sprintf("%s Next: %s", message, e.NextStep)
}
func (e *Error) Unwrap() error { return e.Cause }
func AsError(err error) *Error {
	var target *Error
	if errors.As(err, &target) {
		return target
	}
	return &Error{Code: "INVALID_RESPONSE", Message: err.Error(), Cause: err}
}
func Invalid(message string) error { return &Error{Code: "invalid_input", Message: message} }
func Unsupported(feature string) error {
	return &Error{Code: "unsupported", Message: fmt.Sprintf("Provider does not support %s", feature)}
}
