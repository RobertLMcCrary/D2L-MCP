package d2l

import "github.com/RobertLMcCrary/D2L-MCP/internal/lms"

type ErrorCode = lms.ErrorCode

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

type Error = lms.Error

func AsError(err error) *Error {
	return lms.AsError(err)
}
