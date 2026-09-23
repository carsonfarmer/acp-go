package acp

import (
	"context"
	"encoding/json/jsontext"

	"github.com/ironpark/go-acp/internal/acpconn"
	"github.com/ironpark/go-acp/internal/jsonrpc"
)

// ExtCaller sends requests outside the spec. The agent- and client-side
// connections of every protocol version implement it.
type ExtCaller interface {
	ExtMethod(ctx context.Context, method string, params any) (jsontext.Value, error)
}

// CallExt sends an extension request and decodes its result into an R. A null
// or empty result decodes to the zero value.
//
//	resp, err := acp.CallExt[IndexResponse](ctx, conn, "_zed.dev/index", IndexRequest{Path: dir})
//
// See protocol docs: [Extensibility](https://agentclientprotocol.com/protocol/extensibility)
func CallExt[R any](ctx context.Context, conn ExtCaller, method string, params any) (*R, error) {
	raw, err := conn.ExtMethod(ctx, method, params)
	if err != nil {
		return nil, err
	}
	return acpconn.DecodeResult[R](raw)
}

// ExtRouter serves extension methods and notifications through typed handlers.
// Embed it in an agent or client to implement the façade's ExtMethodHandler
// and ExtNotificationHandler:
//
//	type myAgent struct {
//		acp.ExtRouter
//		// ...
//	}
//
//	acp.HandleExt(&a.ExtRouter, "_zed.dev/index", a.index)
//	acp.OnExtNotification(&a.ExtRouter, "_zed.dev/progress", a.progress)
//
// Params that fail to decode are answered with invalid params. An unregistered
// method is answered with method not found, and an unregistered notification
// is ignored, as the protocol recommends. Register handlers before the
// connection starts; the router is not safe for concurrent registration.
//
// See protocol docs: [Extensibility](https://agentclientprotocol.com/protocol/extensibility)
type ExtRouter struct {
	methods       map[string]jsonrpc.RequestHandler
	notifications map[string]jsonrpc.NotificationHandler
}

// HandleExt registers a typed handler for an extension method. A nil response
// is sent as an empty object.
func HandleExt[P, R any](r *ExtRouter, method string, fn func(context.Context, *P) (*R, error)) {
	if r.methods == nil {
		r.methods = map[string]jsonrpc.RequestHandler{}
	}
	r.methods[method] = func(ctx context.Context, _ string, params jsontext.Value) (any, error) {
		return acpconn.Request(ctx, nil, params, fn)
	}
}

// OnExtNotification registers a typed handler for an extension notification.
func OnExtNotification[P any](r *ExtRouter, method string, fn func(context.Context, *P) error) {
	if r.notifications == nil {
		r.notifications = map[string]jsonrpc.NotificationHandler{}
	}
	r.notifications[method] = func(ctx context.Context, _ string, params jsontext.Value) error {
		return acpconn.Notify(ctx, nil, params, fn)
	}
}

// ExtMethod dispatches an extension request to its registered handler.
func (r *ExtRouter) ExtMethod(ctx context.Context, method string, params jsontext.Value) (any, error) {
	handler, ok := r.methods[method]
	if !ok {
		return nil, ErrMethodNotFound(method)
	}
	return handler(ctx, method, params)
}

// ExtNotification dispatches an extension notification to its registered
// handler, ignoring notifications nothing is registered for.
func (r *ExtRouter) ExtNotification(ctx context.Context, method string, params jsontext.Value) error {
	if handler, ok := r.notifications[method]; ok {
		return handler(ctx, method, params)
	}
	return nil
}
