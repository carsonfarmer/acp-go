package acp

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/ironpark/go-acp/internal/jsonrpc"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// emptyParams stands in for an omitted params member so that requests whose
// fields are all optional still validate.
var emptyParams = jsontext.Value(`{}`)

// decodeParams validates raw against the SDK's Zod rules for T and decodes it.
// Validation failures become -32602 responses, as in the TypeScript SDK.
func decodeParams[T any](raw jsontext.Value) (*T, error) {
	if len(raw) == 0 {
		raw = emptyParams
	}
	value, err := schema.Decode[T](raw)
	if err != nil {
		return nil, jsonrpc.InvalidParams(nil, err.Error())
	}
	return &value, nil
}

// request decodes params and invokes a typed request handler. A nil response
// is encoded as an empty object, matching the reference SDKs' void methods.
func request[T, R any](ctx context.Context, raw jsontext.Value, fn func(context.Context, *T) (*R, error)) (any, error) {
	params, err := decodeParams[T](raw)
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

// notify decodes params and invokes a typed notification handler.
func notify[T any](ctx context.Context, raw jsontext.Value, fn func(context.Context, *T) error) error {
	params, err := decodeParams[T](raw)
	if err != nil {
		return err
	}
	return fn(ctx, params)
}

// call sends a request and decodes its response. A null or empty result
// decodes to the zero value, which is how the protocol spells "void".
//
// Responses are not re-validated: the peer is the authority on its own output,
// and the reference SDKs do not validate them either.
func call[R any](ctx context.Context, conn *jsonrpc.Connection, method string, params any) (*R, error) {
	raw, err := conn.SendRequest(ctx, method, params)
	if err != nil {
		return nil, err
	}
	response := new(R)
	if len(raw) == 0 || string(raw) == "null" {
		return response, nil
	}
	if err := json.Unmarshal(raw, response, json.WithUnmarshalers(schema.Unmarshalers)); err != nil {
		return nil, err
	}
	return response, nil
}
