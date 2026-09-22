// Package acpconn holds what the versioned ACP façades share: connection
// options, the JSON-RPC connection factory and the generic dispatch helpers.
// Each façade aliases these so one option value configures either version.
package acpconn

import (
	"io"
	"time"

	"github.com/ironpark/go-acp/internal/jsonrpc"
)

// Option configures a connection of either protocol version.
type Option func(*Options)

// Options is the resolved option set.
type Options struct {
	Transport jsonrpc.Transport
	JSONRPC   []jsonrpc.Option
}

// Apply folds opts into o.
func (o *Options) Apply(opts []Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithTransport replaces the default stdio transport.
func WithTransport(t jsonrpc.Transport) Option {
	return func(o *Options) { o.Transport = t }
}

// WithErrorHandler sets a callback for non-fatal errors.
func WithErrorHandler(h func(error)) Option {
	return func(o *Options) { o.JSONRPC = append(o.JSONRPC, jsonrpc.WithErrorHandler(h)) }
}

// WithMiddleware adds middleware to the incoming handler chain.
func WithMiddleware(mw ...jsonrpc.Middleware) Option {
	return func(o *Options) { o.JSONRPC = append(o.JSONRPC, jsonrpc.WithMiddleware(mw...)) }
}

// WithWriteQueueSize sets the outgoing queue depth.
func WithWriteQueueSize(size int) Option {
	return func(o *Options) { o.JSONRPC = append(o.JSONRPC, jsonrpc.WithWriteQueueSize(size)) }
}

// WithRequestTimeout bounds outgoing requests whose context has no deadline.
func WithRequestTimeout(d time.Duration) Option {
	return func(o *Options) { o.JSONRPC = append(o.JSONRPC, jsonrpc.WithRequestTimeout(d)) }
}

// WithShutdownTimeout bounds how long Close waits for in-flight handlers.
func WithShutdownTimeout(d time.Duration) Option {
	return func(o *Options) { o.JSONRPC = append(o.JSONRPC, jsonrpc.WithShutdownTimeout(d)) }
}

// NewConnection builds the JSON-RPC connection behind a façade, defaulting to
// newline-delimited JSON over reader and writer.
func NewConnection(request jsonrpc.RequestHandler, notification jsonrpc.NotificationHandler, reader io.Reader, writer io.Writer, opts []Option) *jsonrpc.Connection {
	var o Options
	o.Apply(opts)
	transport := o.Transport
	if transport == nil {
		transport = jsonrpc.NewStdioTransport(reader, writer)
	}
	return jsonrpc.New(request, notification, transport, o.JSONRPC...)
}
