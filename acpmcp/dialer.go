package acpmcp

import (
	"context"
	"encoding/json/jsontext"
	"sync"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// disconnectTimeout bounds the mcp/disconnect sent when a session closes.
const disconnectTimeout = 5 * time.Second

// dialer opens MCP sessions to servers a client provides: the
// version-neutral core of [DialerV1] and [DialerV2].
type dialer struct {
	peer       peer
	connect    func(ctx context.Context, serverID string) (string, error)
	disconnect func(ctx context.Context, connectionID string) error

	mu    sync.Mutex
	conns map[string]*conn
}

func newDialer(p peer, connect func(context.Context, string) (string, error), disconnect func(context.Context, string) error) *dialer {
	return &dialer{peer: p, connect: connect, disconnect: disconnect, conns: map[string]*conn{}}
}

func (d *dialer) dial(ctx context.Context, serverID string, client *mcp.Client, opts *mcp.ClientSessionOptions) (*mcp.ClientSession, error) {
	id, err := d.connect(ctx, serverID)
	if err != nil {
		return nil, err
	}
	c := newConn(id, d.peer, func() {
		d.mu.Lock()
		delete(d.conns, id)
		d.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), disconnectTimeout)
		defer cancel()
		_ = d.disconnect(ctx, id)
	})
	d.mu.Lock()
	d.conns[id] = c
	d.mu.Unlock()
	// Connect negotiates the MCP session over the new connection.
	session, err := client.Connect(ctx, transport{c}, opts)
	if err != nil {
		c.Close()
		return nil, err
	}
	return session, nil
}

func (d *dialer) lookup(connectionID string) (*conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c := d.conns[connectionID]; c != nil {
		return c, nil
	}
	return nil, acp.ErrResourceNotFound("mcp connection " + connectionID)
}

func (d *dialer) message(ctx context.Context, connectionID, method string, params map[string]jsontext.Value) (jsontext.Value, error) {
	c, err := d.lookup(connectionID)
	if err != nil {
		return nil, err
	}
	return c.call(ctx, method, params)
}

func (d *dialer) notifyMessage(connectionID, method string, params map[string]jsontext.Value) error {
	c, err := d.lookup(connectionID)
	if err != nil {
		return err
	}
	return c.notify(method, params)
}
