package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
)

// ErrorCode is a JSON-RPC 2.0 error code.
//
// The values mirror the ACP `ErrorCode` schema, but this package stays
// independent of any protocol version.
type ErrorCode int64

const (
	CodeParseError       ErrorCode = -32700
	CodeInvalidRequest   ErrorCode = -32600
	CodeMethodNotFound   ErrorCode = -32601
	CodeInvalidParams    ErrorCode = -32602
	CodeInternalError    ErrorCode = -32603
	CodeRequestCancelled ErrorCode = -32800
	CodeAuthRequired     ErrorCode = -32000
	CodeResourceNotFound ErrorCode = -32002
)

// RequestError is a JSON-RPC error object carried as a Go error.
//
// Handlers return it to control the code, message and data of the error
// response; peer errors are returned to callers in the same shape.
type RequestError struct {
	Code    ErrorCode
	Message string
	// Data is marshaled into the error object's "data" member. It is nil when
	// the peer sent no data.
	Data any
}

func (e *RequestError) Error() string {
	if e.Data != nil {
		return fmt.Sprintf("jsonrpc error %d: %s (data: %v)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// suffix appends ": detail" to a base message, mirroring the reference SDKs.
func suffix(base string, detail []string) string {
	if len(detail) > 0 && detail[0] != "" {
		return base + ": " + detail[0]
	}
	return base
}

// ParseError reports invalid JSON received by the peer (-32700).
func ParseError(data any, detail ...string) *RequestError {
	return &RequestError{Code: CodeParseError, Message: suffix("Parse error", detail), Data: data}
}

// InvalidRequest reports a malformed request object (-32600).
func InvalidRequest(data any, detail ...string) *RequestError {
	return &RequestError{Code: CodeInvalidRequest, Message: suffix("Invalid request", detail), Data: data}
}

// MethodNotFound reports an unknown or unsupported method (-32601).
func MethodNotFound(method string) *RequestError {
	return &RequestError{
		Code:    CodeMethodNotFound,
		Message: fmt.Sprintf("Method not found: %s", method),
		Data:    map[string]string{"method": method},
	}
}

// InvalidParams reports parameters that failed validation (-32602).
func InvalidParams(data any, detail ...string) *RequestError {
	return &RequestError{Code: CodeInvalidParams, Message: suffix("Invalid params", detail), Data: data}
}

// InternalError reports a handler failure (-32603).
func InternalError(data any, detail ...string) *RequestError {
	return &RequestError{Code: CodeInternalError, Message: suffix("Internal error", detail), Data: data}
}

// RequestCancelled reports that a request was cancelled before completing (-32800).
//
// See the [request cancellation RFD].
//
// [request cancellation RFD]: https://agentclientprotocol.com/protocol/rfds/request-cancellation
func RequestCancelled(data any, detail ...string) *RequestError {
	return &RequestError{Code: CodeRequestCancelled, Message: suffix("Request cancelled", detail), Data: data}
}

// AuthRequired reports that the caller must authenticate first (-32000).
func AuthRequired(data any, detail ...string) *RequestError {
	return &RequestError{Code: CodeAuthRequired, Message: suffix("Authentication required", detail), Data: data}
}

// ResourceNotFound reports a missing resource such as a file (-32002).
func ResourceNotFound(uri ...string) *RequestError {
	if len(uri) > 0 && uri[0] != "" {
		return &RequestError{
			Code:    CodeResourceNotFound,
			Message: "Resource not found: " + uri[0],
			Data:    map[string]string{"uri": uri[0]},
		}
	}
	return &RequestError{Code: CodeResourceNotFound, Message: "Resource not found"}
}

// wireError is the JSON-RPC error object as it appears on the wire.
type wireError struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"message"`
	Data    jsontext.Value `json:"data,omitzero"`
}

func (e *wireError) toRequestError() *RequestError {
	err := &RequestError{Code: e.Code, Message: e.Message}
	if len(e.Data) > 0 {
		err.Data = e.Data.Clone()
	}
	return err
}

func (e *RequestError) toWire() *wireError {
	out := &wireError{Code: e.Code, Message: e.Message}
	if e.Data == nil {
		return out
	}
	data, err := json.Marshal(e.Data)
	if err != nil {
		// Keep the error response well-formed even if the payload is not.
		data, _ = json.Marshal(fmt.Sprint(e.Data))
	}
	out.Data = data
	return out
}
