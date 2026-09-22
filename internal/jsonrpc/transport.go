package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json/jsontext"
	"io"
	"sync"
)

const (
	// maxMessageSize is the largest single message the stdio transport accepts (50MB).
	maxMessageSize = 50 * 1024 * 1024
	// initialBufSize is the initial read buffer size (64KB).
	initialBufSize = 64 * 1024
)

// Transport is a bidirectional message transport for JSON-RPC communication.
//
// Implementations own framing for their layer (stdio, HTTP+SSE, in-memory
// pipes). [Connection] reads and writes whole JSON values through it.
type Transport interface {
	// ReadMessage returns the next message, or io.EOF once the peer is gone.
	// A nil value with a nil error means "nothing this round" (for example a
	// blank line) and is skipped by the connection.
	ReadMessage(ctx context.Context) (jsontext.Value, error)

	// WriteMessage sends one encoded message.
	WriteMessage(ctx context.Context, data jsontext.Value) error

	// Close releases the transport's resources.
	Close() error
}

// StdioTransport carries newline-delimited JSON over an io.Reader/io.Writer pair.
//
// This is the default transport for ACP over stdio. Reads and writes ignore
// context cancellation because the underlying streams have no cancellation
// mechanism; the connection's read loop checks for cancellation between reads.
type StdioTransport struct {
	reader  io.Reader
	scanner *bufio.Scanner
	writer  io.Writer
	mu      sync.Mutex
}

// NewStdioTransport creates a newline-delimited JSON transport.
func NewStdioTransport(reader io.Reader, writer io.Writer) *StdioTransport {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, initialBufSize), maxMessageSize)
	return &StdioTransport{reader: reader, scanner: scanner, writer: writer}
}

func (t *StdioTransport) ReadMessage(context.Context) (jsontext.Value, error) {
	if !t.scanner.Scan() {
		if err := t.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	data := t.scanner.Bytes()
	if len(data) == 0 {
		return nil, nil
	}
	return jsontext.Value(data).Clone(), nil
}

var newline = []byte{'\n'}

func (t *StdioTransport) WriteMessage(_ context.Context, data jsontext.Value) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, err := t.writer.Write(data); err != nil {
		return err
	}
	_, err := t.writer.Write(newline)
	return err
}

func (t *StdioTransport) Close() error {
	var errs [2]error
	if closer, ok := t.reader.(io.Closer); ok {
		errs[0] = closer.Close()
	}
	if closer, ok := t.writer.(io.Closer); ok {
		errs[1] = closer.Close()
	}
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
