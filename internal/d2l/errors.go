package d2l

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	ErrConfig          ErrorCode = "CONFIG_REQUIRED"
	ErrAuth            ErrorCode = "AUTH_REQUIRED"
	ErrForbidden       ErrorCode = "FORBIDDEN"
	ErrNotFound        ErrorCode = "NOT_FOUND"
	ErrAmbiguous       ErrorCode = "AMBIGUOUS_MATCH"
	ErrRateLimited     ErrorCode = "RATE_LIMITED"
	ErrInvalidResponse ErrorCode = "INVALID_RESPONSE"
	ErrUnsafePath      ErrorCode = "UNSAFE_PATH"
	ErrTooLarge        ErrorCode = "TOO_LARGE"
)

type Error struct {
	Code       ErrorCode `json:"code"`
	Message    string    `json:"message"`
	NextStep   string    `json:"next_step,omitempty"`
	StatusCode int       `json:"status_code,omitempty"`
	Cause      error     `json:"-"`
}

func (e *Error) Error() string {
	if e.NextStep == "" {
		return e.Message
	}

	return fmt.Sprintf("%s Next: %s", e.Message, e.NextStep)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func AsError(err error) *Error {
	var target *Error

	if errors.As(err, &target) {
		return target
	}

	return &Error{
		Code:    ErrInvalidResponse,
		Message: err.Error(),
		Cause:   err,
	}
}
