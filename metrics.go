package acp

import (
	"context"
	"encoding/json/jsontext"
	"time"
)

// RPCKind is the kind of incoming message a [Metrics] observation describes:
// a request, which expects a response, or a notification.
type RPCKind string

const (
	// RPCRequest is a request expecting a response.
	RPCRequest RPCKind = "request"
	// RPCNotification is a notification, which has no response.
	RPCNotification RPCKind = "notification"
)

// Metrics observes every message a connection handles, so one hook can feed a
// metrics backend or start and end tracing spans. The context is the handler's
// own, so a tracing implementation can annotate the span it derived from it.
//
// It only sees incoming traffic: use [MetricsMiddleware] on a connection to
// instrument what the peer sends. Outgoing calls have no shared hook; count
// them where they are made.
type Metrics interface {
	ObserveRPC(ctx context.Context, method string, kind RPCKind, duration time.Duration, err error)
}

// MetricsFunc adapts a function to [Metrics].
type MetricsFunc func(ctx context.Context, method string, kind RPCKind, duration time.Duration, err error)

// ObserveRPC calls f.
func (f MetricsFunc) ObserveRPC(ctx context.Context, method string, kind RPCKind, duration time.Duration, err error) {
	f(ctx, method, kind, duration, err)
}

// MetricsMiddleware reports every handled message to m with its method, kind,
// duration and any error, so one hook covers counters, histograms and tracing:
//
//	mw := acp.MetricsMiddleware(acp.MetricsFunc(func(ctx context.Context, method string, kind acp.RPCKind, d time.Duration, err error) {
//		rpcCalls.WithLabelValues(method, string(kind)).Inc()
//		rpcDuration.WithLabelValues(method).Observe(d.Seconds())
//	}))
//	conn := acp1.NewAgentSideConnection(newAgent, transport, acp.WithMiddleware(mw))
//
// A nil m makes the middleware a no-op, and the observation still carries the
// error a request handler returned, not the [RequestError] the peer receives.
func MetricsMiddleware(m Metrics) Middleware {
	if m == nil {
		return Middleware{}
	}
	return Middleware{
		Request: func(next RequestHandler) RequestHandler {
			return func(ctx context.Context, method string, params jsontext.Value) (any, error) {
				start := time.Now()
				result, err := next(ctx, method, params)
				m.ObserveRPC(ctx, method, RPCRequest, time.Since(start), err)
				return result, err
			}
		},
		Notification: func(next NotificationHandler) NotificationHandler {
			return func(ctx context.Context, method string, params jsontext.Value) error {
				start := time.Now()
				err := next(ctx, method, params)
				m.ObserveRPC(ctx, method, RPCNotification, time.Since(start), err)
				return err
			}
		},
	}
}
