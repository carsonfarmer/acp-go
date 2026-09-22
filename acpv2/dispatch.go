package acpv2

import (
	"context"
	"encoding/json/jsontext"

	"github.com/ironpark/go-acp/internal/acpconn"
	"github.com/ironpark/go-acp/internal/jsonrpc"
	schema "github.com/ironpark/go-acp/schema/v2"
)

// The dispatch helpers are shared with the v2 façade; only the validation
// rules differ, so these bind the v1 rules and nothing else.

func request[T, R any](ctx context.Context, raw jsontext.Value, fn func(context.Context, *T) (*R, error)) (any, error) {
	return acpconn.Request(ctx, schema.Validated, raw, fn)
}

func notify[T any](ctx context.Context, raw jsontext.Value, fn func(context.Context, *T) error) error {
	return acpconn.Notify(ctx, schema.Validated, raw, fn)
}

func call[R any](ctx context.Context, conn *jsonrpc.Connection, method string, params any) (*R, error) {
	return acpconn.Call[R](ctx, conn, method, params)
}
