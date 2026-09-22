package acpv2

import (
	"io"
	"time"

	"github.com/ironpark/go-acp/internal/acpconn"
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

// Option configures a connection. It is the same type as the v1 package's
// Option, so a transport or middleware set up once serves either version.
type Option = acpconn.Option

// WithTransport replaces the default stdio transport. The reader and writer
// passed to the constructor are then unused.
func WithTransport(t Transport) Option { return acpconn.WithTransport(t) }

// WithErrorHandler sets a callback for non-fatal errors: undecodable messages,
// write failures and errors returned by notification handlers.
func WithErrorHandler(h func(error)) Option { return acpconn.WithErrorHandler(h) }

// WithMiddleware adds middleware to the incoming handler chain.
func WithMiddleware(mw ...Middleware) Option { return acpconn.WithMiddleware(mw...) }

// WithWriteQueueSize sets the outgoing queue depth. Default: 100.
func WithWriteQueueSize(size int) Option { return acpconn.WithWriteQueueSize(size) }

// WithRequestTimeout bounds outgoing requests whose caller context carries no
// deadline of its own. Default: none.
func WithRequestTimeout(d time.Duration) Option { return acpconn.WithRequestTimeout(d) }

// WithShutdownTimeout bounds how long Close waits for in-flight handlers.
// Default: wait indefinitely.
func WithShutdownTimeout(d time.Duration) Option { return acpconn.WithShutdownTimeout(d) }
