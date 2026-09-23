package acp

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"time"
)

// HTTPClientTransport is the client side of Streamable HTTP. It needs no setup
// beyond the endpoint URL: the first message it sends must be initialize,
// whose reply carries the connection id; it then opens the connection stream,
// and a session's stream as soon as a message names the session.
//
// Close deletes the connection on the server. A connection never closes its
// transport, so the caller does, after the connection stops.
type HTTPClientTransport struct {
	url string
	httpClientConfig

	inbox       chan jsontext.Value
	done        chan struct{}
	closeOnce   sync.Once
	streamCtx   context.Context
	stopStreams context.CancelFunc
	streams     sync.WaitGroup
	streamEnded chan struct{} // closed once the connection stream stops
	readErr     error         // why it stopped: io.EOF, or a read error

	mu           sync.Mutex
	connectionID string
	sessions     map[string]bool   // sessions whose stream is open
	pendingLoads map[string]string // session/load request id -> session
}

// HTTPClientOption configures [NewHTTPClientTransport] and
// [DialWebSocket].
type HTTPClientOption func(*httpClientConfig)

type httpClientConfig struct {
	client *http.Client
	header http.Header
}

// newHTTPClientConfig applies opts over the defaults: a client with a cookie
// jar, since the protocol requires clients to keep the server's cookies for
// the connection.
func newHTTPClientConfig(opts []HTTPClientOption) httpClientConfig {
	jar, _ := cookiejar.New(nil)
	c := httpClientConfig{client: &http.Client{Jar: jar}, header: http.Header{}}
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// WithHTTPClient sets the HTTP client. The protocol requires clients to keep
// cookies for the connection, so give it a Jar; the default client has one.
// Its Timeout must be zero, since the streams stay open.
func WithHTTPClient(client *http.Client) HTTPClientOption {
	return func(c *httpClientConfig) { c.client = client }
}

// WithHTTPHeader adds a header to every request, such as Authorization.
func WithHTTPHeader(key, value string) HTTPClientOption {
	return func(c *httpClientConfig) { c.header.Add(key, value) }
}

// NewHTTPClientTransport returns a transport to the ACP endpoint at url, such
// as "https://agent.example.com/acp".
func NewHTTPClientTransport(url string, opts ...HTTPClientOption) *HTTPClientTransport {
	ctx, cancel := context.WithCancel(context.Background())
	return &HTTPClientTransport{
		url:              url,
		httpClientConfig: newHTTPClientConfig(opts),
		inbox:            make(chan jsontext.Value, streamBuffer),
		done:             make(chan struct{}),
		streamCtx:        ctx,
		stopStreams:      cancel,
		streamEnded:      make(chan struct{}),
		sessions:         map[string]bool{},
		pendingLoads:     map[string]string{},
	}
}

// ReadMessage returns the next message from the agent. Once the connection
// stream ends, it returns the messages already received and then io.EOF, or
// the error that ended the stream, so the connection stops instead of waiting
// for replies that cannot arrive. A session stream ending is not an error.
func (t *HTTPClientTransport) ReadMessage(ctx context.Context) (jsontext.Value, error) {
	select {
	case msg := <-t.inbox:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.done:
		return nil, io.EOF
	case <-t.streamEnded:
		select {
		case msg := <-t.inbox:
			return msg, nil
		case <-t.done:
			return nil, io.EOF // Close cancelled the stream; that is not a read error
		default:
			return nil, t.readErr
		}
	}
}

// WriteMessage posts one message to the agent.
func (t *HTTPClientTransport) WriteMessage(ctx context.Context, data jsontext.Value) error {
	select {
	case <-t.done:
		return ErrTransportClosed
	default:
	}
	e, err := parseEnvelope(data)
	if err != nil {
		return err
	}
	if e.Method == initializeMethod && e.isRequest() {
		t.mu.Lock()
		initialized := t.connectionID != ""
		t.mu.Unlock()
		if initialized {
			return errors.New("acp: initialize already sent on this transport")
		}
		return t.initialize(ctx, data)
	}

	t.mu.Lock()
	connectionID := t.connectionID
	session := e.paramsSession()
	loading := e.isRequest() && e.Method == loadSessionMethod && session != ""
	if loading {
		t.pendingLoads[e.idKey()] = session
	}
	t.mu.Unlock()
	if connectionID == "" {
		return fmt.Errorf("acp: %s sent before initialize", e.Method)
	}

	header := http.Header{ConnectionIDHeader: {connectionID}}
	if sessionScopedMethods[e.Method] && session != "" {
		header.Set(SessionIDHeader, session)
	}
	resp, err := t.post(ctx, data, header)
	if err == nil {
		err = t.accepted(resp)
	}
	if err != nil && loading {
		t.mu.Lock()
		delete(t.pendingLoads, e.idKey())
		t.mu.Unlock()
	}
	return err
}

// initialize posts the initialize request, whose reply comes back in the
// response body with the connection id, then opens the connection stream.
func (t *HTTPClientTransport) initialize(ctx context.Context, data jsontext.Value) error {
	resp, err := t.post(ctx, data, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError("initialize", resp)
	}
	connectionID := resp.Header.Get(ConnectionIDHeader)
	if connectionID == "" {
		return fmt.Errorf("acp: initialize response has no %s header", ConnectionIDHeader)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMessageSize))
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.connectionID = connectionID
	t.mu.Unlock()
	t.receive(jsontext.Value(body))
	t.openStream("")
	return nil
}

func (t *HTTPClientTransport) post(ctx context.Context, data jsontext.Value, header http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	t.setHeaders(req, header)
	req.Header.Set("Content-Type", "application/json")
	return t.client.Do(req)
}

// accepted checks a POST's status. 202 is the norm; a 200 body is a reply.
func (t *HTTPClientTransport) accepted(resp *http.Response) error {
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil
	case http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxMessageSize))
		if err == nil && len(bytes.TrimSpace(body)) > 0 {
			t.receive(jsontext.Value(body))
		}
		return err
	}
	return statusError("POST", resp)
}

func statusError(what string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
	return fmt.Errorf("acp: %s: HTTP %d: %s", what, resp.StatusCode, bytes.TrimSpace(body))
}

func (t *HTTPClientTransport) setHeaders(req *http.Request, header http.Header) {
	for k, v := range t.header {
		req.Header[k] = v
	}
	for k, v := range header {
		req.Header[k] = v
	}
}

// openStream starts reading the connection stream (session "") or a
// session's stream, once per session.
func (t *HTTPClientTransport) openStream(session string) {
	t.mu.Lock()
	if session != "" {
		if t.sessions[session] {
			t.mu.Unlock()
			return
		}
		t.sessions[session] = true
	}
	connectionID := t.connectionID
	t.mu.Unlock()

	t.streams.Go(func() {
		err := t.readStream(connectionID, session)
		if session != "" {
			t.mu.Lock()
			delete(t.sessions, session)
			t.mu.Unlock()
			return
		}
		// The connection stream is the only way replies arrive: losing it
		// ends the transport for reading.
		t.readErr = io.EOF
		if err != nil {
			t.readErr = err
		}
		close(t.streamEnded)
	})
}

func (t *HTTPClientTransport) readStream(connectionID, session string) error {
	req, err := http.NewRequestWithContext(t.streamCtx, http.MethodGet, t.url, nil)
	if err != nil {
		return err
	}
	header := http.Header{"Accept": {"text/event-stream"}, ConnectionIDHeader: {connectionID}}
	if session != "" {
		header.Set(SessionIDHeader, session)
	}
	t.setHeaders(req, header)
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("acp: open event stream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError("open event stream", resp)
	}
	if err := readSSE(resp.Body, t.receive); err != nil {
		return fmt.Errorf("acp: event stream: %w", err)
	}
	return nil
}

// receive queues a message from the agent, opening the stream of any
// session it names.
func (t *HTTPClientTransport) receive(msg jsontext.Value) {
	if e, err := parseEnvelope(msg); err == nil {
		t.mu.Lock()
		var open []string
		if e.isResponse() {
			key := e.idKey()
			if loaded, ok := t.pendingLoads[key]; ok {
				delete(t.pendingLoads, key)
				if len(e.Result) > 0 {
					open = append(open, loaded)
				}
			}
		}
		session := e.paramsSession()
		if session == "" {
			session = e.resultSession()
		}
		// A load's replay arrives on the connection stream; its session
		// stream opens once the load succeeds, not for a load that fails.
		if session != "" && !t.loading(session) {
			open = append(open, session)
		}
		t.mu.Unlock()
		for _, s := range open {
			t.openStream(s)
		}
	}
	select {
	case t.inbox <- msg:
	case <-t.done:
	}
}

func (t *HTTPClientTransport) loading(session string) bool {
	for _, s := range t.pendingLoads {
		if s == session {
			return true
		}
	}
	return false
}

// Close stops the streams and deletes the connection on the server.
func (t *HTTPClientTransport) Close() error {
	t.closeOnce.Do(func() {
		close(t.done)
		t.stopStreams()
		t.mu.Lock()
		connectionID := t.connectionID
		t.mu.Unlock()
		if connectionID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.url, nil); err == nil {
				t.setHeaders(req, http.Header{ConnectionIDHeader: {connectionID}})
				if resp, err := t.client.Do(req); err == nil {
					resp.Body.Close()
				}
			}
		}
		t.streams.Wait()
	})
	return nil
}
