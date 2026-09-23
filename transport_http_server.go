package acp

import (
	"context"
	"crypto/rand"
	"encoding/json/jsontext"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// initializeTimeout bounds how long the initialize POST waits for the
	// agent's response, which it returns in its body.
	initializeTimeout = 30 * time.Second
	// keepaliveInterval is how often an idle SSE stream sends a comment, so
	// proxies do not time it out. It matches the TypeScript and Python SDKs.
	keepaliveInterval = 15 * time.Second
)

// HTTPServer serves agents over Streamable HTTP and WebSocket on one
// endpoint. Each initialize request, or each WebSocket, starts a connection
// and calls serve with its [Transport]; serve runs the agent on it and returns
// when the connection ends:
//
//	server := acp.NewHTTPServer(func(ctx context.Context, t acp.Transport) error {
//		conn := acpv1.NewAgentSideConnection(newAgent, nil, nil, acp.WithTransport(t))
//		return conn.Start(ctx)
//	})
//	http.Handle("/acp", server)
//
// For an agent that speaks several protocol versions, serve can hand the
// transport to a router.ProtocolRouter:
//
//	acp.NewHTTPServer(func(ctx context.Context, t acp.Transport) error { return r.Serve(ctx, t) })
//
// ctx is cancelled when the client deletes the connection or the server
// closes.
type HTTPServer struct {
	serve   func(ctx context.Context, t Transport) error
	origins []string

	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	conns   map[string]*httpServerConn
	sockets map[string]*WebSocketTransport
}

// HTTPServerOption configures [NewHTTPServer].
type HTTPServerOption func(*HTTPServer)

// WithWebSocketOrigins lets browser pages on other origins open WebSockets to
// the server. Without it only pages on the server's own host may, which keeps
// other sites from driving the agent with a visitor's cookies. Patterns match
// the Origin host with path.Match, or "scheme://host" if they contain "://".
func WithWebSocketOrigins(patterns ...string) HTTPServerOption {
	return func(s *HTTPServer) { s.origins = append(s.origins, patterns...) }
}

// NewHTTPServer returns a handler that runs serve once per connection.
func NewHTTPServer(serve func(ctx context.Context, t Transport) error, opts ...HTTPServerOption) *HTTPServer {
	ctx, cancel := context.WithCancel(context.Background())
	s := &HTTPServer{
		serve: serve, ctx: ctx, cancel: cancel,
		conns: map[string]*httpServerConn{}, sockets: map[string]*WebSocketTransport{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Close ends every connection. It does not stop the http.Server using it.
func (s *HTTPServer) Close() error {
	s.cancel()
	s.mu.Lock()
	conns, sockets := s.conns, s.sockets
	s.conns, s.sockets = map[string]*httpServerConn{}, map[string]*WebSocketTransport{}
	s.mu.Unlock()
	for _, c := range conns {
		c.Close()
	}
	for _, t := range sockets {
		t.Close()
	}
	return nil
}

// track registers a WebSocket connection so Close can end it, reporting false
// once the server is closed.
func (s *HTTPServer) track(id string, t *WebSocketTransport) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return false
	}
	s.sockets[id] = t
	return true
}

func (s *HTTPServer) untrack(id string) {
	s.mu.Lock()
	delete(s.sockets, id)
	s.mu.Unlock()
}

// ServeHTTP routes POST, GET and DELETE on the ACP endpoint, and GETs asking
// to upgrade to WebSocket.
func (s *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.post(w, r)
	case http.MethodGet:
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			s.websocket(w, r)
			return
		}
		s.stream(w, r)
	case http.MethodDelete:
		s.delete(w, r)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *HTTPServer) connection(w http.ResponseWriter, r *http.Request) (*httpServerConn, bool) {
	id := r.Header.Get(ConnectionIDHeader)
	if id == "" {
		http.Error(w, "missing "+ConnectionIDHeader, http.StatusBadRequest)
		return nil, false
	}
	s.mu.Lock()
	c := s.conns[id]
	s.mu.Unlock()
	if c == nil {
		http.Error(w, "unknown connection", http.StatusNotFound)
		return nil, false
	}
	return c, true
}

func (s *HTTPServer) remove(id string) {
	s.mu.Lock()
	c := s.conns[id]
	delete(s.conns, id)
	s.mu.Unlock()
	if c != nil {
		c.Close()
	}
}

func (s *HTTPServer) post(w http.ResponseWriter, r *http.Request) {
	if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxMessageSize))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	msg := jsontext.Value(body)
	if msg.Kind() == '[' {
		http.Error(w, "batch requests are not supported", http.StatusNotImplemented)
		return
	}
	e, err := parseEnvelope(msg)
	if err != nil {
		http.Error(w, "invalid JSON-RPC message", http.StatusBadRequest)
		return
	}
	if e.Method == initializeMethod && e.isRequest() && r.Header.Get(ConnectionIDHeader) == "" {
		s.initialize(w, r, msg, e)
		return
	}

	c, ok := s.connection(w, r)
	if !ok {
		return
	}
	session := r.Header.Get(SessionIDHeader)
	if sessionScopedMethods[e.Method] && session == "" {
		http.Error(w, "missing "+SessionIDHeader, http.StatusBadRequest)
		return
	}
	if session != "" && !c.hasSession(session) {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	if err := c.deliver(r.Context(), msg, e); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *HTTPServer) initialize(w http.ResponseWriter, r *http.Request, msg jsontext.Value, e envelope) {
	c := newHTTPServerConn(rand.Text(), e.idKey())
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		http.Error(w, "server closed", http.StatusServiceUnavailable)
		return
	}
	s.conns[c.id] = c
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(s.ctx)
	go func() {
		defer cancel()
		_ = s.serve(ctx, c)
		s.remove(c.id)
	}()
	// The connection ends with its transport: a DELETE or Close stops serve.
	go func() {
		select {
		case <-c.done:
			cancel()
		case <-ctx.Done():
		}
	}()

	fail := func(status int, text string) {
		s.remove(c.id)
		http.Error(w, text, status)
	}
	if err := c.deliver(r.Context(), msg, e); err != nil {
		fail(http.StatusServiceUnavailable, err.Error())
		return
	}
	timer := time.NewTimer(initializeTimeout)
	defer timer.Stop()
	select {
	case response := <-c.initResponse:
		w.Header().Set(ConnectionIDHeader, c.id)
		w.Header().Set("Content-Type", "application/json")
		w.Write(response)
	case <-timer.C:
		fail(http.StatusGatewayTimeout, "initialize timed out")
	case <-c.done:
		fail(http.StatusInternalServerError, "connection closed during initialize")
	case <-r.Context().Done():
		s.remove(c.id)
	}
}

func (s *HTTPServer) stream(w http.ResponseWriter, r *http.Request) {
	if !acceptsEventStream(r.Header.Values("Accept")) {
		http.Error(w, "Accept must include text/event-stream", http.StatusNotAcceptable)
		return
	}
	c, ok := s.connection(w, r)
	if !ok {
		return
	}
	out := c.stream(r.Header.Get(SessionIDHeader))
	if out == nil {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	if !out.attach() {
		http.Error(w, "stream already open", http.StatusConflict)
		return
	}
	defer out.detach()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	keepalive := time.NewTicker(keepaliveInterval)
	defer keepalive.Stop()
	for {
		select {
		case msg := <-out.messages:
			if writeSSE(w, msg) != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-out.closed:
			return
		case <-r.Context().Done():
			return
		}
	}
}

func acceptsEventStream(values []string) bool {
	for _, v := range values {
		for part := range strings.SplitSeq(v, ",") {
			if mediaType, _, _ := mime.ParseMediaType(strings.TrimSpace(part)); mediaType == "text/event-stream" {
				return true
			}
		}
	}
	return false
}

func (s *HTTPServer) delete(w http.ResponseWriter, r *http.Request) {
	c, ok := s.connection(w, r)
	if !ok {
		return
	}
	s.remove(c.id)
	w.WriteHeader(http.StatusAccepted)
}

// httpServerConn is the [Transport] of one Streamable HTTP connection. POSTed
// messages are read by the agent; what the agent writes goes to the
// connection or a session stream.
type httpServerConn struct {
	id           string
	initID       string
	initResponse chan jsontext.Value
	incoming     chan jsontext.Value
	done         chan struct{}
	closeOnce    sync.Once

	mu            sync.Mutex
	connStream    *outbound
	sessions      map[string]*outbound
	pendingRoutes map[string]string // request id -> session its reply goes to
	pendingLoads  map[string]string // session/load request id -> session
	provisional   map[string]bool   // session streams opened for a load in flight
}

func newHTTPServerConn(id, initID string) *httpServerConn {
	return &httpServerConn{
		id:            id,
		initID:        initID,
		initResponse:  make(chan jsontext.Value, 1),
		incoming:      make(chan jsontext.Value),
		done:          make(chan struct{}),
		connStream:    newOutbound(),
		sessions:      map[string]*outbound{},
		pendingRoutes: map[string]string{},
		pendingLoads:  map[string]string{},
		provisional:   map[string]bool{},
	}
}

func (c *httpServerConn) hasSession(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[id] != nil
}

// stream returns the connection stream for "", or the session's stream.
func (c *httpServerConn) stream(session string) *outbound {
	if session == "" {
		return c.connStream
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[session]
}

// deliver hands a POSTed message to the agent, first noting where the reply
// to a session request must go.
func (c *httpServerConn) deliver(ctx context.Context, msg jsontext.Value, e envelope) error {
	if e.isRequest() {
		if session := e.paramsSession(); session != "" {
			c.mu.Lock()
			if e.Method == loadSessionMethod {
				c.pendingLoads[e.idKey()] = session
				if c.sessions[session] == nil {
					// Let the client open the stream while the replay runs.
					c.sessions[session] = newOutbound()
					c.provisional[session] = true
				}
			} else {
				c.pendingRoutes[e.idKey()] = session
			}
			c.mu.Unlock()
		}
	}
	select {
	case c.incoming <- msg:
		return nil
	case <-c.done:
		return ErrTransportClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *httpServerConn) ReadMessage(ctx context.Context) (jsontext.Value, error) {
	select {
	case msg := <-c.incoming:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, io.EOF
	}
}

func (c *httpServerConn) WriteMessage(ctx context.Context, data jsontext.Value) error {
	select {
	case <-c.done:
		return ErrTransportClosed
	default:
	}
	e, err := parseEnvelope(data)
	if err != nil {
		return err
	}
	if e.isResponse() && e.idKey() == c.initID {
		select {
		case c.initResponse <- data.Clone():
			return nil
		default:
			return errors.New("duplicate initialize response")
		}
	}
	return c.route(e).push(ctx, data.Clone(), c.done)
}

// route picks the stream for a message the agent sends.
func (c *httpServerConn) route(e envelope) *outbound {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := e.paramsSession()
	if e.isResponse() {
		key := e.idKey()
		if loaded, ok := c.pendingLoads[key]; ok {
			delete(c.pendingLoads, key)
			if len(e.Result) > 0 {
				delete(c.provisional, loaded)
			} else if c.provisional[loaded] && !c.loading(loaded) {
				delete(c.provisional, loaded)
				c.sessions[loaded].close()
				delete(c.sessions, loaded)
			}
			return c.connStream
		}
		session = c.pendingRoutes[key]
		delete(c.pendingRoutes, key)
		if created := e.resultSession(); created != "" {
			// session/new, fork or resume: the client learns the id from this
			// reply on the connection stream, then opens the session stream.
			if c.sessions[created] == nil {
				c.sessions[created] = newOutbound()
			}
			delete(c.provisional, created)
			return c.connStream
		}
	}
	// A load's replay stays on the connection stream, ordered with its reply.
	if session == "" || c.loading(session) {
		return c.connStream
	}
	if out := c.sessions[session]; out != nil {
		return out
	}
	return c.connStream
}

// loading reports whether a session/load for session awaits its reply.
func (c *httpServerConn) loading(session string) bool {
	for _, s := range c.pendingLoads {
		if s == session {
			return true
		}
	}
	return false
}

func (c *httpServerConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.mu.Lock()
		defer c.mu.Unlock()
		c.connStream.close()
		for _, out := range c.sessions {
			out.close()
		}
	})
	return nil
}

// outbound buffers the messages of one SSE stream until its GET reads them.
type outbound struct {
	messages  chan jsontext.Value
	closed    chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	attached  bool
}

func newOutbound() *outbound {
	return &outbound{messages: make(chan jsontext.Value, streamBuffer), closed: make(chan struct{})}
}

// push queues msg, waiting while the buffer is full: dropping a reply would
// leave the peer's request pending forever.
func (o *outbound) push(ctx context.Context, msg jsontext.Value, done <-chan struct{}) error {
	select {
	case o.messages <- msg:
		return nil
	case <-o.closed:
		return ErrTransportClosed
	case <-done:
		return ErrTransportClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// attach claims the stream for one GET at a time.
func (o *outbound) attach() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.attached {
		return false
	}
	o.attached = true
	return true
}

func (o *outbound) detach() {
	o.mu.Lock()
	o.attached = false
	o.mu.Unlock()
}

func (o *outbound) close() { o.closeOnce.Do(func() { close(o.closed) }) }
