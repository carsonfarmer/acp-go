package acpv2

import "github.com/ironpark/go-acp/internal/jsonrpc"

// RequestError is a JSON-RPC error carried as a Go error.
//
// Returning one from a handler controls the code, message and data the peer
// receives; any other error becomes an internal error. Errors from the peer
// are returned to callers in this same shape, so errors.As recovers the code.
type RequestError = jsonrpc.RequestError

// ErrorCode is a JSON-RPC error code.
type ErrorCode = jsonrpc.ErrorCode

// Error codes defined by JSON-RPC 2.0 and by ACP.
const (
	ErrorCodeParseError       = jsonrpc.CodeParseError
	ErrorCodeInvalidRequest   = jsonrpc.CodeInvalidRequest
	ErrorCodeMethodNotFound   = jsonrpc.CodeMethodNotFound
	ErrorCodeInvalidParams    = jsonrpc.CodeInvalidParams
	ErrorCodeInternalError    = jsonrpc.CodeInternalError
	ErrorCodeRequestCancelled = jsonrpc.CodeRequestCancelled
	ErrorCodeAuthRequired     = jsonrpc.CodeAuthRequired
	ErrorCodeResourceNotFound = jsonrpc.CodeResourceNotFound
)

// ErrParseError reports invalid JSON (-32700).
func ErrParseError(data any, detail ...string) *RequestError {
	return jsonrpc.ParseError(data, detail...)
}

// ErrInvalidRequest reports a malformed request object (-32600).
func ErrInvalidRequest(data any, detail ...string) *RequestError {
	return jsonrpc.InvalidRequest(data, detail...)
}

// ErrMethodNotFound reports an unknown or unsupported method (-32601).
//
// The connection returns this automatically when an optional method's
// interface is not implemented.
func ErrMethodNotFound(method string) *RequestError {
	return jsonrpc.MethodNotFound(method)
}

// ErrInvalidParams reports parameters that failed validation (-32602).
func ErrInvalidParams(data any, detail ...string) *RequestError {
	return jsonrpc.InvalidParams(data, detail...)
}

// ErrInternalError reports a handler failure (-32603).
func ErrInternalError(data any, detail ...string) *RequestError {
	return jsonrpc.InternalError(data, detail...)
}

// ErrRequestCancelled reports a request abandoned before completion (-32800).
//
// The connection returns this automatically when a handler's context is
// cancelled, either by the peer's $/cancel_request or by shutdown.
func ErrRequestCancelled(data any, detail ...string) *RequestError {
	return jsonrpc.RequestCancelled(data, detail...)
}

// ErrAuthRequired reports that the caller must authenticate first (-32000).
//
// Agents return this from NewSession when no credentials are available yet.
func ErrAuthRequired(data any, detail ...string) *RequestError {
	return jsonrpc.AuthRequired(data, detail...)
}

// ErrResourceNotFound reports a missing resource such as a file (-32002).
func ErrResourceNotFound(uri ...string) *RequestError {
	return jsonrpc.ResourceNotFound(uri...)
}
