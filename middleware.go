package acp

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"log/slog"
	"time"
)

// LoggingMiddleware logs each incoming method to logger with its duration and
// any error, as structured attributes: "method", "duration", and on failure
// "error" and, for a [RequestError], "code". A request logs at Info and a
// notification, which session updates make frequent, at Debug; a failure of
// either logs at Warn. The handler's context is passed to logger.
//
// Passing nil uses [slog.Default].
func LoggingMiddleware(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	log := func(ctx context.Context, level slog.Level, msg, method string, start time.Time, err error) {
		attrs := []slog.Attr{slog.String("method", method), slog.Duration("duration", time.Since(start))}
		if err != nil {
			level = slog.LevelWarn
			attrs = append(attrs, slog.Any("error", err))
			if rpcErr, ok := errors.AsType[*RequestError](err); ok {
				attrs = append(attrs, slog.Int("code", int(rpcErr.Code)))
			}
		}
		logger.LogAttrs(ctx, level, msg, attrs...)
	}
	return Middleware{
		Request: func(next RequestHandler) RequestHandler {
			return func(ctx context.Context, method string, params jsontext.Value) (any, error) {
				start := time.Now()
				result, err := next(ctx, method, params)
				log(ctx, slog.LevelInfo, "acp request", method, start, err)
				return result, err
			}
		},
		Notification: func(next NotificationHandler) NotificationHandler {
			return func(ctx context.Context, method string, params jsontext.Value) error {
				start := time.Now()
				err := next(ctx, method, params)
				log(ctx, slog.LevelDebug, "acp notification", method, start, err)
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
