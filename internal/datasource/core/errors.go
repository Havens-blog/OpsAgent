// Package core provides error types for data source operations.
package core

import (
	"fmt"
	"time"
)

// ErrorType represents the type of error that occurred.
type ErrorType string

const (
	// ErrTypeAuth represents authentication errors.
	ErrTypeAuth ErrorType = "auth"
	// ErrTypePermission represents permission errors.
	ErrTypePermission ErrorType = "permission"
	// ErrTypeRateLimit represents rate limit errors.
	ErrTypeRateLimit ErrorType = "rate_limit"
	// ErrTypeTimeout represents timeout errors.
	ErrTypeTimeout ErrorType = "timeout"
	// ErrTypeNetwork represents network errors.
	ErrTypeNetwork ErrorType = "network"
	// ErrTypeParsing represents parsing errors.
	ErrTypeParsing ErrorType = "parsing"
	// ErrTypeValidation represents validation errors.
	ErrTypeValidation ErrorType = "validation"
	// ErrTypeQuota represents quota errors.
	ErrTypeQuota ErrorType = "quota"
	// ErrTypeNotFound represents not found errors.
	ErrTypeNotFound ErrorType = "not_found"
	// ErrTypeInternal represents internal errors.
	ErrTypeInternal ErrorType = "internal"
	// ErrTypeUnknown represents unknown errors.
	ErrTypeUnknown ErrorType = "unknown"
)

// DataSourceError is the base error type for all data source errors.
type DataSourceError struct {
	// Type is the type of error.
	Type ErrorType `json:"type"`
	// Message is the error message.
	Message string `json:"message"`
	// DataSource is the data source that produced the error.
	DataSource string `json:"data_source,omitempty"`
	// Operation is the operation that was being performed.
	Operation string `json:"operation,omitempty"`
	// Retryable indicates if the error is retryable.
	Retryable bool `json:"retryable"`
	// Temporary indicates if the error is temporary.
	Temporary bool `json:"temporary"`
	// Cause is the underlying error.
	Cause error `json:"cause,omitempty"`
	// Timestamp is when the error occurred.
	Timestamp time.Time `json:"timestamp"`
	// RequestID is the ID of the request that failed (if available).
	RequestID string `json:"request_id,omitempty"`
}

// Error implements the error interface.
func (e *DataSourceError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s error in %s: %s: %v", e.Type, e.Operation, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s error in %s: %s", e.Type, e.Operation, e.Message)
}

// Unwrap returns the underlying cause.
func (e *DataSourceError) Unwrap() error {
	return e.Cause
}

// IsRetryable returns true if the error is retryable.
func (e *DataSourceError) IsRetryable() bool {
	return e.Retryable
}

// IsTemporary returns true if the error is temporary.
func (e *DataSourceError) IsTemporary() bool {
	return e.Temporary
}

// GetErrorType returns the error type.
func (e *DataSourceError) GetErrorType() ErrorType {
	return e.Type
}

// NewAuthError creates a new authentication error.
func NewAuthError(dataSource, operation, message string, cause error) *DataSourceError {
	return &DataSourceError{
		Type:       ErrTypeAuth,
		Message:    message,
		DataSource: dataSource,
		Operation:  operation,
		Retryable:  false,
		Temporary:  false,
		Cause:      cause,
		Timestamp:  time.Now(),
	}
}

// NewPermissionError creates a new permission error.
func NewPermissionError(dataSource, operation, message string, cause error) *DataSourceError {
	return &DataSourceError{
		Type:       ErrTypePermission,
		Message:    message,
		DataSource: dataSource,
		Operation:  operation,
		Retryable:  false,
		Temporary:  false,
		Cause:      cause,
		Timestamp:  time.Now(),
	}
}

// NewRateLimitError creates a new rate limit error.
func NewRateLimitError(dataSource, operation string, cause error) *DataSourceError {
	return &DataSourceError{
		Type:       ErrTypeRateLimit,
		Message:    "rate limit exceeded",
		DataSource: dataSource,
		Operation:  operation,
		Retryable:  true,
		Temporary:  true,
		Cause:      cause,
		Timestamp:  time.Now(),
	}
}

// NewTimeoutError creates a new timeout error.
func NewTimeoutError(dataSource, operation string, cause error) *DataSourceError {
	return &DataSourceError{
		Type:       ErrTypeTimeout,
		Message:    "operation timed out",
		DataSource: dataSource,
		Operation:  operation,
		Retryable:  true,
		Temporary:  true,
		Cause:      cause,
		Timestamp:  time.Now(),
	}
}

// NewNetworkError creates a new network error.
func NewNetworkError(dataSource, operation string, cause error) *DataSourceError {
	return &DataSourceError{
		Type:       ErrTypeNetwork,
		Message:    "network error",
		DataSource: dataSource,
		Operation:  operation,
		Retryable:  true,
		Temporary:  true,
		Cause:      cause,
		Timestamp:  time.Now(),
	}
}

// NewParsingError creates a new parsing error.
func NewParsingError(dataSource, operation, message string, cause error) *DataSourceError {
	return &DataSourceError{
		Type:       ErrTypeParsing,
		Message:    message,
		DataSource: dataSource,
		Operation:  operation,
		Retryable:  false,
		Temporary:  false,
		Cause:      cause,
		Timestamp:  time.Now(),
	}
}

// NewValidationError creates a new validation error.
func NewValidationError(dataSource, operation, message string) *DataSourceError {
	return &DataSourceError{
		Type:       ErrTypeValidation,
		Message:    message,
		DataSource: dataSource,
		Operation:  operation,
		Retryable:  false,
		Temporary:  false,
		Timestamp:  time.Now(),
	}
}

// NewQuotaError creates a new quota error.
func NewQuotaError(dataSource, operation, message string, cause error) *DataSourceError {
	return &DataSourceError{
		Type:       ErrTypeQuota,
		Message:    message,
		DataSource: dataSource,
		Operation:  operation,
		Retryable:  false,
		Temporary:  false,
		Cause:      cause,
		Timestamp:  time.Now(),
	}
}

// NewNotFoundError creates a new not found error.
func NewNotFoundError(dataSource, operation, resource string) *DataSourceError {
	return &DataSourceError{
		Type:       ErrTypeNotFound,
		Message:    fmt.Sprintf("resource not found: %s", resource),
		DataSource: dataSource,
		Operation:  operation,
		Retryable:  false,
		Temporary:  false,
		Timestamp:  time.Now(),
	}
}

// NewInternalError creates a new internal error.
func NewInternalError(dataSource, operation string, cause error) *DataSourceError {
	return &DataSourceError{
		Type:       ErrTypeInternal,
		Message:    "internal error",
		DataSource: dataSource,
		Operation:  operation,
		Retryable:  false,
		Temporary:  false,
		Cause:      cause,
		Timestamp:  time.Now(),
	}
}

// NewUnknownError creates a new unknown error.
func NewUnknownError(dataSource, operation string, cause error) *DataSourceError {
	return &DataSourceError{
		Type:       ErrTypeUnknown,
		Message:    "unknown error",
		DataSource: dataSource,
		Operation:  operation,
		Retryable:  false,
		Temporary:  false,
		Cause:      cause,
		Timestamp:  time.Now(),
	}
}

// WrapError wraps an error with data source context.
func WrapError(dataSource, operation string, err error) *DataSourceError {
	if err == nil {
		return nil
	}

	// If it's already a DataSourceError, just update context
	if dsErr, ok := err.(*DataSourceError); ok {
		if dsErr.DataSource == "" {
			dsErr.DataSource = dataSource
		}
		if dsErr.Operation == "" {
			dsErr.Operation = operation
		}
		return dsErr
	}

	// Otherwise, create a new error based on the error message
	return NewUnknownError(dataSource, operation, err)
}

// IsRetryable checks if an error is retryable.
func IsRetryable(err error) bool {
	if dsErr, ok := err.(*DataSourceError); ok {
		return dsErr.IsRetryable()
	}
	return false
}

// IsTemporary checks if an error is temporary.
func IsTemporary(err error) bool {
	if dsErr, ok := err.(*DataSourceError); ok {
		return dsErr.IsTemporary()
	}
	return false
}

// GetErrorType extracts the error type from an error.
func GetErrorType(err error) ErrorType {
	if dsErr, ok := err.(*DataSourceError); ok {
		return dsErr.GetErrorType()
	}
	return ErrTypeUnknown
}
