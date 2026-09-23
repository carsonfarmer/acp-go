package acpmcp

import (
	"context"
	"crypto/rand"
	"encoding/json/jsontext"
	"sync"

	acp "github.com/ironpark/acp-go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// host serves the MCP servers a client provides: the version-neutral core of
// [HostV1] and [HostV2].
type host struct {
	peer peer

	mu      sync.Mutex
	servers map[string]*mcp.Server
	conns   map[string]*hostConn
}

type hostConn struct {
	*conn
	session *mcp.ServerSession
}

func newHost(p peer) *host {
	return &host{peer: p, servers: map[string]*mcp.Server{}, conns: map[string]*hostConn{}}
}

// add registers server and returns the id the agent connects to it by.
func (h *host) add(server *mcp.Server) string {
	id := rand.Text()
	h.mu.Lock()
	h.servers[id] = server
	h.mu.Unlock()
	return id
}

func (h *host) connect(ctx context.Context, serverID string) (string, error) {
	h.mu.Lock()
	server := h.servers[serverID]
	h.mu.Unlock()
	if server == nil {
		return "", acp.ErrResourceNotFound("mcp server " + serverID)
	}
	id := rand.Text()
	c := newConn(id, h.peer, func() {
		h.mu.Lock()
		delete(h.conns, id)
		h.mu.Unlock()
	})
	// The session outlives this request; the connection's close ends it.
	session, err := server.Connect(context.WithoutCancel(ctx), transport{c}, nil)
	if err != nil {
		return "", acp.ErrInternalError(err.Error())
	}
	h.mu.Lock()
	h.conns[id] = &hostConn{conn: c, session: session}
	h.mu.Unlock()
	return id, nil
}

func (h *host) lookup(connectionID string) (*hostConn, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c := h.conns[connectionID]; c != nil {
		return c, nil
	}
	return nil, acp.ErrResourceNotFound("mcp connection " + connectionID)
}

func (h *host) message(ctx context.Context, connectionID, method string, params map[string]jsontext.Value) (jsontext.Value, error) {
	c, err := h.lookup(connectionID)
	if err != nil {
		return nil, err
	}
	return c.call(ctx, method, params)
}

func (h *host) notifyMessage(connectionID, method string, params map[string]jsontext.Value) error {
	c, err := h.lookup(connectionID)
	if err != nil {
		return err
	}
	return c.notify(method, params)
}

func (h *host) disconnect(connectionID string) error {
	c, err := h.lookup(connectionID)
	if err != nil {
		return err
	}
	return c.session.Close() // closes the conn too
}
