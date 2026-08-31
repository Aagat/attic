package application

import (
	"errors"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	ErrNotTerminal         = errors.New("job is not terminal")
	ErrLeaseLost           = errors.New("job lease lost")
	ErrInvalidStage        = errors.New("stage transition is invalid")
)

// SafeError is the only error shape that crosses the JobArchive seam.  Its
// message is deliberately safe for clients; implementation details stay in
// the unexported cause.
type SafeError struct {
	Code          string
	Message       string
	HTTPStatus    int
	CorrelationID string
	Retryable     bool
	cause         error
}

func (e *SafeError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *SafeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *SafeError) WithCorrelationID(id string) *SafeError {
	if e == nil {
		return nil
	}
	copy := *e
	copy.CorrelationID = id
	return &copy
}

func NewSafeError(code string, status int, message string) *SafeError {
	return &SafeError{Code: code, HTTPStatus: status, Message: message}
}

func internalError(cause error) *SafeError {
	return &SafeError{
		Code:       "internal_error",
		HTTPStatus: 500,
		Message:    "The operation could not be completed",
		cause:      cause,
	}
}

func safeError(err error) *SafeError {
	if err == nil {
		return nil
	}
	var safe *SafeError
	if errors.As(err, &safe) && safe != nil {
		return safe
	}
	return internalError(err)
}

// ProcessingError is used by a processor adapter to return a stable job
// failure category without exposing provider or browser details.
type ProcessingError struct {
	Category  string
	Message   string
	Retryable bool
	cause     error
}

func (e *ProcessingError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *ProcessingError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func NewProcessingError(category string, message string, retryable bool) *ProcessingError {
	return &ProcessingError{Category: category, Message: message, Retryable: retryable}
}
