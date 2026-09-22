package acp

import (
	"io"
	"time"

	"github.com/ironpark/go-acp/internal/jsonrpc"
)

// Transport is a bidirectional message transport. Implement it to carry ACP
// over something other than stdio, such as HTTP+SSE or an in-process pipe.
type Transport = jsonrpc.Transport

// Middleware wraps incoming request and/or notification handling. Either field
// may be nil; the first middleware added is the outermost.
type Middleware = jsonrpc.Middleware

// RequestHandler handles one incoming request and returns the value to encode
// as its result.
type RequestHandler = jsonrpc.RequestHandler

// NotificationHandler handles one incoming notification.
type NotificationHandler = jsonrpc.NotificationHandler

// NewStdioTransport carries newline-delimited JSON over a reader/writer pair.
func NewStdioTransport(reader io.Reader, writer io.Writer) *jsonrpc.StdioTransport {
	return jsonrpc.NewStdioTransport(reader, writer)
}

// Option configures a connection.
type Option func(*options)

type options struct {
	transport Transport
	jsonrpc   []jsonrpc.Option
}

func (o *options) apply(opts []Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithTransport replaces the default stdio transport. The reader and writer
// passed to the constructor are then unused.
func WithTransport(t Transport) Option {
	return func(o *options) { o.transport = t }
}

// WithErrorHandler sets a callback for non-fatal errors: undecodable messages,
// write failures and errors returned by notification handlers.
func WithErrorHandler(h func(error)) Option {
	return func(o *options) { o.jsonrpc = append(o.jsonrpc, jsonrpc.WithErrorHandler(h)) }
}

// WithMiddleware adds middleware to the incoming handler chain.
func WithMiddleware(mw ...Middleware) Option {
	return func(o *options) { o.jsonrpc = append(o.jsonrpc, jsonrpc.WithMiddleware(mw...)) }
}

// WithWriteQueueSize sets the outgoing queue depth. Default: 100.
func WithWriteQueueSize(size int) Option {
	return func(o *options) { o.jsonrpc = append(o.jsonrpc, jsonrpc.WithWriteQueueSize(size)) }
}

// WithRequestTimeout bounds outgoing requests whose caller context carries no
// deadline of its own. Default: none.
func WithRequestTimeout(d time.Duration) Option {
	return func(o *options) { o.jsonrpc = append(o.jsonrpc, jsonrpc.WithRequestTimeout(d)) }
}

// WithShutdownTimeout bounds how long Close waits for in-flight handlers.
// Default: wait indefinitely.
func WithShutdownTimeout(d time.Duration) Option {
	return func(o *options) { o.jsonrpc = append(o.jsonrpc, jsonrpc.WithShutdownTimeout(d)) }
}

// newConnection builds the JSON-RPC connection shared by both façades.
func newConnection(request RequestHandler, notification NotificationHandler, reader io.Reader, writer io.Writer, o *options) *jsonrpc.Connection {
	transport := o.transport
	if transport == nil {
		transport = jsonrpc.NewStdioTransport(reader, writer)
	}
	return jsonrpc.New(request, notification, transport, o.jsonrpc...)
}
