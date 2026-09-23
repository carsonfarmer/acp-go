package acp

import (
	"context"
	"crypto/rand"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/coder/websocket"
)

// WebSocketTransport carries ACP over one WebSocket, the second profile of the
// Streamable HTTP endpoint: a GET with "Upgrade: websocket" on the same URL.
// Each text frame is one JSON-RPC message, in both directions; binary frames
// are ignored. The first message is still initialize.
//
// [HTTPServer] accepts WebSocket upgrades; [DialWebSocket] is the client side.
type WebSocketTransport struct {
	conn      *websocket.Conn
	closeOnce sync.Once
	closeErr  error
}

func newWebSocketTransport(conn *websocket.Conn) *WebSocketTransport {
	conn.SetReadLimit(maxMessageSize)
	return &WebSocketTransport{conn: conn}
}

// DialWebSocket connects to the ACP endpoint at url over WebSocket, such as
// "wss://agent.example.com/acp". The transport is open when DialWebSocket
// returns; Close closes the socket, which ends the connection on the server.
func DialWebSocket(ctx context.Context, url string, opts ...HTTPClientOption) (*WebSocketTransport, error) {
	cfg := newHTTPClientConfig(opts)
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPClient: cfg.client,
		HTTPHeader: cfg.header,
	})
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("acp: websocket upgrade: HTTP %d: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("acp: websocket: %w", err)
	}
	return newWebSocketTransport(conn), nil
}

// ReadMessage returns the next text frame, or io.EOF once the socket closes.
func (t *WebSocketTransport) ReadMessage(ctx context.Context) (jsontext.Value, error) {
	for {
		kind, data, err := t.conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if websocket.CloseStatus(err) != -1 || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil, io.EOF
			}
			return nil, err
		}
		if kind == websocket.MessageText {
			return jsontext.Value(data), nil
		}
	}
}

// WriteMessage sends msg as one text frame.
func (t *WebSocketTransport) WriteMessage(ctx context.Context, msg jsontext.Value) error {
	if err := t.conn.Write(ctx, websocket.MessageText, msg); err != nil {
		if websocket.CloseStatus(err) != -1 || errors.Is(err, net.ErrClosed) {
			return ErrTransportClosed
		}
		return err
	}
	return nil
}

// Close closes the socket with a normal closure.
func (t *WebSocketTransport) Close() error {
	t.closeOnce.Do(func() {
		t.closeErr = t.conn.Close(websocket.StatusNormalClosure, "")
		if websocket.CloseStatus(t.closeErr) != -1 || errors.Is(t.closeErr, net.ErrClosed) {
			t.closeErr = nil // the peer closed first
		}
	})
	return t.closeErr
}

// websocket upgrades a GET to a WebSocket and serves one connection on it,
// until either side closes the socket.
func (s *HTTPServer) websocket(w http.ResponseWriter, r *http.Request) {
	id := rand.Text()
	w.Header().Set(ConnectionIDHeader, id)
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.origins})
	if err != nil {
		return // Accept has answered the request
	}
	t := newWebSocketTransport(conn)
	if !s.track(id, t) {
		conn.Close(websocket.StatusGoingAway, "server closed")
		return
	}
	defer s.untrack(id)

	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	_ = s.serve(ctx, t)
	t.Close()
}
