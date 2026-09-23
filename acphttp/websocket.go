package acphttp

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
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// WebSocketTransport carries ACP over one WebSocket, the second profile of the
// Streamable HTTP endpoint: a GET with "Upgrade: websocket" on the same URL.
// Each text frame is one JSON-RPC message, in both directions; binary frames
// are ignored. The first message is still initialize.
//
// [Server] accepts WebSocket upgrades; [DialWebSocket] is the client side.
//
// Both sides ping the peer every 15 seconds and close the socket when a pong
// does not arrive within the next 15, so a peer that vanished without closing
// is noticed; [WithWebSocketPing] and [WithPingInterval] change the interval.
// Pongs are sent while the peer reads, which an ACP connection always does.
type WebSocketTransport struct {
	conn      *websocket.Conn
	stopPing  context.CancelFunc
	lost      atomic.Pointer[error] // why pinging closed the socket
	closeOnce sync.Once
	closeErr  error
}

func newWebSocketTransport(conn *websocket.Conn, pingInterval time.Duration) *WebSocketTransport {
	conn.SetReadLimit(maxMessageSize)
	ctx, cancel := context.WithCancel(context.Background())
	t := &WebSocketTransport{conn: conn, stopPing: cancel}
	if pingInterval > 0 {
		go t.ping(ctx, pingInterval)
	}
	return t
}

// ping checks the peer every interval until ctx ends, closing the socket
// when it stops answering.
func (t *WebSocketTransport) ping(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		pingCtx, cancel := context.WithTimeout(ctx, interval)
		err := t.conn.Ping(pingCtx)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				err = fmt.Errorf("acp: websocket peer not responding: %w", err)
				t.lost.Store(&err)
				t.conn.CloseNow()
			}
			return
		}
	}
}

// DialWebSocket connects to the ACP endpoint at url over WebSocket, such as
// "wss://agent.example.com/acp". The transport is open when DialWebSocket
// returns; Close closes the socket, which ends the connection on the server.
func DialWebSocket(ctx context.Context, url string, opts ...ClientOption) (*WebSocketTransport, error) {
	cfg := newClientConfig(opts)
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPClient: cfg.client,
		HTTPHeader: cfg.header,
	})
	if err != nil {
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			return nil, &StatusError{Op: "websocket upgrade", StatusCode: resp.StatusCode, Err: err}
		}
		return nil, fmt.Errorf("acp: websocket: %w", err)
	}
	return newWebSocketTransport(conn, cfg.pingInterval), nil
}

// ReadMessage returns the next text frame, or io.EOF once the socket closes.
// It returns an error instead if the peer stopped answering pings.
func (t *WebSocketTransport) ReadMessage(ctx context.Context) (jsontext.Value, error) {
	for {
		kind, data, err := t.conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if lost := t.lost.Load(); lost != nil {
				return nil, *lost
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
		t.stopPing()
		t.closeErr = t.conn.Close(websocket.StatusNormalClosure, "")
		if websocket.CloseStatus(t.closeErr) != -1 || errors.Is(t.closeErr, net.ErrClosed) {
			t.closeErr = nil // the peer closed first
		}
	})
	return t.closeErr
}

// websocket upgrades a GET to a WebSocket and serves one connection on it,
// until either side closes the socket.
func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	id := rand.Text()
	w.Header().Set(ConnectionIDHeader, id)
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.origins})
	if err != nil {
		return // Accept has answered the request
	}
	t := newWebSocketTransport(conn, s.pingInterval)
	ended := make(chan struct{})
	_, ok := s.start(r, t, func() { s.sockets[id] = t }, func() {
		s.untrack(id)
		t.Close()
		close(ended)
	})
	if !ok {
		conn.Close(websocket.StatusGoingAway, "server closed")
		return
	}
	// The handler holds the upgraded connection until serve is done with it.
	<-ended
}
