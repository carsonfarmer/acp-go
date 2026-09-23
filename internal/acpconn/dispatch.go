package acpconn

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/ironpark/acp-go/internal/jsonrpc"
)

// emptyParams stands in for an omitted params member so that requests whose
// fields are all optional still validate.
var emptyParams = jsontext.Value(`{}`)

// decodeParams decodes raw into a T, applying the schema version's Zod rules
// carried by validated. Failures become -32602, as in the TypeScript SDK.
func decodeParams[T any](validated json.Options, raw jsontext.Value) (*T, error) {
	if len(raw) == 0 {
		raw = emptyParams
	}
	params := new(T)
	if err := json.Unmarshal(raw, params, validated); err != nil {
		return nil, jsonrpc.InvalidParams(nil, err.Error())
	}
	return params, nil
}

// Request decodes params and invokes a typed request handler. A nil response
// is encoded as an empty object, matching the reference SDKs' void methods.
func Request[T, R any](ctx context.Context, validated json.Options, raw jsontext.Value, fn func(context.Context, *T) (*R, error)) (any, error) {
	params, err := decodeParams[T](validated, raw)
	if err != nil {
		return nil, err
	}
	response, err := fn(ctx, params)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, nil
	}
	return response, nil
}

// Notify decodes params and invokes a typed notification handler.
func Notify[T any](ctx context.Context, validated json.Options, raw jsontext.Value, fn func(context.Context, *T) error) error {
	params, err := decodeParams[T](validated, raw)
	if err != nil {
		return err
	}
	return fn(ctx, params)
}

// Call sends a request and decodes its response. A null or empty result
// decodes to the zero value, which is how the protocol spells "void".
//
// Responses are not validated: the peer is the authority on its own output,
// and the reference SDKs do not validate them either.
func Call[R any](ctx context.Context, conn *jsonrpc.Connection, method string, params any) (*R, error) {
	raw, err := conn.SendRequest(ctx, method, params)
	if err != nil {
		return nil, err
	}
	return DecodeResult[R](raw)
}

// DecodeResult decodes a response result, treating null or empty as the zero
// value.
func DecodeResult[R any](raw jsontext.Value) (*R, error) {
	response := new(R)
	if len(raw) == 0 || string(raw) == "null" {
		return response, nil
	}
	if err := json.Unmarshal(raw, response); err != nil {
		return nil, err
	}
	return response, nil
}
