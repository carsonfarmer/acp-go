package acp

import (
	"context"
	"encoding/json/jsontext"
	"log"
	"time"
)

// LoggingMiddleware logs each incoming method, how long it took and any error.
//
// Passing nil uses log.Printf.
func LoggingMiddleware(logger func(format string, args ...any)) Middleware {
	if logger == nil {
		logger = log.Printf
	}
	return Middleware{
		Request: func(next RequestHandler) RequestHandler {
			return func(ctx context.Context, method string, params jsontext.Value) (any, error) {
				start := time.Now()
				result, err := next(ctx, method, params)
				if err != nil {
					logger("[ACP] %s failed (%s): %v", method, time.Since(start), err)
				} else {
					logger("[ACP] %s completed (%s)", method, time.Since(start))
				}
				return result, err
			}
		},
		Notification: func(next NotificationHandler) NotificationHandler {
			return func(ctx context.Context, method string, params jsontext.Value) error {
				err := next(ctx, method, params)
				if err != nil {
					logger("[ACP] %s (notification) failed: %v", method, err)
				} else {
					logger("[ACP] %s (notification)", method)
				}
				return err
			}
		},
	}
}

// TimeoutMiddleware bounds how long any one handler may run. Handlers should
// watch ctx and return promptly; a handler that returns after its deadline
// answers the peer with a cancelled error.
func TimeoutMiddleware(timeout time.Duration) Middleware {
	return Middleware{
		Request: func(next RequestHandler) RequestHandler {
			return func(ctx context.Context, method string, params jsontext.Value) (any, error) {
				ctx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				return next(ctx, method, params)
			}
		},
		Notification: func(next NotificationHandler) NotificationHandler {
			return func(ctx context.Context, method string, params jsontext.Value) error {
				ctx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				return next(ctx, method, params)
			}
		},
	}
}

// MethodFilterMiddleware applies mw only to methods matching filter, so a
// policy can target a few methods without wrapping the whole connection.
func MethodFilterMiddleware(filter func(method string) bool, mw Middleware) Middleware {
	out := Middleware{}
	if mw.Request != nil {
		out.Request = func(next RequestHandler) RequestHandler {
			filtered := mw.Request(next)
			return func(ctx context.Context, method string, params jsontext.Value) (any, error) {
				if filter(method) {
					return filtered(ctx, method, params)
				}
				return next(ctx, method, params)
			}
		}
	}
	if mw.Notification != nil {
		out.Notification = func(next NotificationHandler) NotificationHandler {
			filtered := mw.Notification(next)
			return func(ctx context.Context, method string, params jsontext.Value) error {
				if filter(method) {
					return filtered(ctx, method, params)
				}
				return next(ctx, method, params)
			}
		}
	}
	return out
}
