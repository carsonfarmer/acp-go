package main

import (
	"fmt"
)

// ErrorType represents different types of errors that can occur
type ErrorType string

const (
	ErrorTypeValidation    ErrorType = "validation"
	ErrorTypeFileSystem    ErrorType = "filesystem"
	ErrorTypeGeneration    ErrorType = "generation"
	ErrorTypeSerialization ErrorType = "serialization"
	ErrorTypeCLI           ErrorType = "cli"
)

// AppError represents a structured application error
type AppError struct {
	Type    ErrorType
	Message string
	Cause   error
	Context map[string]any
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Cause
}

// WithContext adds context information to the error
func (e *AppError) WithContext(key string, value any) *AppError {
	if e.Context == nil {
		e.Context = make(map[string]any)
	}
	e.Context[key] = value
	return e
}

// NewError creates a new application error
func NewError(errorType ErrorType, message string, cause error) *AppError {
	return &AppError{
		Type:    errorType,
		Message: message,
		Cause:   cause,
		Context: make(map[string]any),
	}
}

// NewValidationError creates a validation error
func NewValidationError(message string, cause error) *AppError {
	return NewError(ErrorTypeValidation, message, cause)
}

// NewFileSystemError creates a filesystem error
func NewFileSystemError(message string, cause error) *AppError {
	return NewError(ErrorTypeFileSystem, message, cause)
}

// NewGenerationError creates a generation error
func NewGenerationError(message string, cause error) *AppError {
	return NewError(ErrorTypeGeneration, message, cause)
}

// NewSerializationError creates a serialization error
func NewSerializationError(message string, cause error) *AppError {
	return NewError(ErrorTypeSerialization, message, cause)
}

// NewCLIError creates a CLI error
func NewCLIError(message string, cause error) *AppError {
	return NewError(ErrorTypeCLI, message, cause)
}

// IsErrorType checks if an error is of a specific type
func IsErrorType(err error, errorType ErrorType) bool {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Type == errorType
	}
	return false
}