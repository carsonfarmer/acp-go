package acp

import (
	"errors"

	"github.com/ironpark/acp-go/internal/jsonrpc"
)

// RequestError is a JSON-RPC error carried as a Go error.
//
// Returning one from a handler controls the code, message and data the peer
// receives; any other error becomes an internal error. Errors from the peer
// are returned to callers in this same shape, so errors.As recovers the code.
type RequestError = jsonrpc.RequestError

// IsCode reports whether err is, or wraps, a [RequestError] with the given
// code:
//
//	if acp.IsCode(err, acp.ErrorCodeAuthRequired) {
//		// authenticate, then retry
//	}
func IsCode(err error, code ErrorCode) bool {
	var reqErr *RequestError
	return errors.As(err, &reqErr) && reqErr.Code == code
}

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

// The constructors below build the standard errors. Each takes a detail
// that follows the standard message ("Invalid params: unknown mode") and may
// be empty; use [RequestError.WithData] to attach a "data" member:
//
//	return nil, acp.InvalidParams(fmt.Sprintf("unknown mode %q", id))

// ParseError reports invalid JSON (-32700).
func ParseError(detail string) *RequestError { return jsonrpc.ParseError(detail) }

// InvalidRequest reports a malformed request object (-32600).
func InvalidRequest(detail string) *RequestError { return jsonrpc.InvalidRequest(detail) }

// MethodNotFound reports an unknown or unsupported method (-32601).
//
// The connection returns this automatically when an optional method's
// interface is not implemented.
func MethodNotFound(method string) *RequestError { return jsonrpc.MethodNotFound(method) }

// InvalidParams reports parameters that failed validation (-32602).
func InvalidParams(detail string) *RequestError { return jsonrpc.InvalidParams(detail) }

// InternalError reports a handler failure (-32603).
func InternalError(detail string) *RequestError { return jsonrpc.InternalError(detail) }

// RequestCancelled reports a request abandoned before completion (-32800).
//
// The connection returns this automatically when a handler's context is
// cancelled, either by the peer's $/cancel_request or by shutdown.
func RequestCancelled(detail string) *RequestError { return jsonrpc.RequestCancelled(detail) }

// AuthRequired reports that the caller must authenticate first (-32000).
//
// Agents return this from NewSession when no credentials are available yet.
func AuthRequired(detail string) *RequestError { return jsonrpc.AuthRequired(detail) }

// ResourceNotFound reports a missing resource such as a file (-32002).
func ResourceNotFound(detail string) *RequestError { return jsonrpc.ResourceNotFound(detail) }
