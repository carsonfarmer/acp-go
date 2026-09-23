package acpmcp

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"sync"

	acp "github.com/ironpark/acp-go"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// peer sends MCP traffic to the other end of the ACP connection over
// mcp/message, as a request or a notification.
type peer struct {
	request func(ctx context.Context, connectionID, method string, params map[string]jsontext.Value) (jsontext.Value, error)
	notify  func(ctx context.Context, connectionID, method string, params map[string]jsontext.Value) error
}

// conn is one MCP connection carried over ACP: the [mcp.Connection] of the
// local MCP server or client, whose messages it trades with the peer.
//
// A request the local side writes goes to the peer as an mcp/message
// request, and the peer's answer comes back as the response under the
// request's own id. A request from the peer is read by the local side under
// an id conn mints; its response resolves the peer's mcp/message call. The
// minted ids only ever label messages conn writes itself, so they cannot
// clash with the local side's own.
type conn struct {
	id      string
	peer    peer
	ctx     context.Context // ends when the connection closes
	cancel  context.CancelFunc
	onClose func()
	once    sync.Once

	mu      sync.Mutex
	queue   []jsonrpc.Message // read by the local side, in order
	ready   chan struct{}     // signalled when queue gains a message
	nextID  int
	waiting map[string]chan *jsonrpc.Response // minted id -> the peer's call
}

func newConn(id string, p peer, onClose func()) *conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &conn{
		id: id, peer: p, ctx: ctx, cancel: cancel, onClose: onClose,
		ready: make(chan struct{}, 1), waiting: map[string]chan *jsonrpc.Response{},
	}
}

var errClosed = errors.New("acpmcp: connection closed")

// push queues msg for the local side without blocking, so a peer's message
// never stalls the ACP connection's read loop.
func (c *conn) push(msg jsonrpc.Message) {
	c.mu.Lock()
	c.queue = append(c.queue, msg)
	c.mu.Unlock()
	select {
	case c.ready <- struct{}{}:
	default:
	}
}

func (c *conn) Read(ctx context.Context) (jsonrpc.Message, error) {
	for {
		c.mu.Lock()
		if len(c.queue) > 0 {
			msg := c.queue[0]
			c.queue = c.queue[1:]
			c.mu.Unlock()
			return msg, nil
		}
		c.mu.Unlock()
		select {
		case <-c.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.ctx.Done():
			return nil, io.EOF
		}
	}
}

func (c *conn) Write(ctx context.Context, msg jsonrpc.Message) error {
	if c.ctx.Err() != nil {
		return errClosed
	}
	switch msg := msg.(type) {
	case *jsonrpc.Request:
		params, err := toParams(msg.Params)
		if err != nil {
			return err
		}
		if !msg.ID.IsValid() {
			return c.peer.notify(ctx, c.id, msg.Method, params)
		}
		// The answer arrives later as a response; Write must not wait for it.
		go func() {
			result, err := c.peer.request(c.ctx, c.id, msg.Method, params)
			c.push(&jsonrpc.Response{ID: msg.ID, Result: json.RawMessage(result), Error: toWireError(err)})
		}()
		return nil
	case *jsonrpc.Response:
		key := fmt.Sprint(msg.ID.Raw())
		c.mu.Lock()
		call := c.waiting[key]
		delete(c.waiting, key)
		c.mu.Unlock()
		if call != nil {
			call <- msg
		}
		return nil
	}
	return fmt.Errorf("acpmcp: unexpected message %T", msg)
}

func (c *conn) Close() error {
	c.once.Do(func() {
		c.cancel()
		if c.onClose != nil {
			c.onClose()
		}
	})
	return nil
}

func (c *conn) SessionID() string { return "" }

// call hands the local side a request from the peer and returns its result.
func (c *conn) call(ctx context.Context, method string, params map[string]jsontext.Value) (jsontext.Value, error) {
	raw, err := fromParams(params)
	if err != nil {
		return nil, err
	}
	answer := make(chan *jsonrpc.Response, 1)
	c.mu.Lock()
	c.nextID++
	key := fmt.Sprintf("acp-%d", c.nextID)
	c.waiting[key] = answer
	c.mu.Unlock()
	id, _ := jsonrpc.MakeID(key)
	c.push(&jsonrpc.Request{ID: id, Method: method, Params: raw})

	select {
	case response := <-answer:
		if response.Error != nil {
			return nil, toRequestError(response.Error)
		}
		if len(response.Result) == 0 {
			return jsontext.Value(`{}`), nil
		}
		return jsontext.Value(response.Result), nil
	case <-ctx.Done():
		c.forget(key)
		return nil, ctx.Err()
	case <-c.ctx.Done():
		c.forget(key)
		return nil, errClosed
	}
}

func (c *conn) forget(key string) {
	c.mu.Lock()
	delete(c.waiting, key)
	c.mu.Unlock()
}

// notify hands the local side a notification from the peer.
func (c *conn) notify(method string, params map[string]jsontext.Value) error {
	raw, err := fromParams(params)
	if err != nil {
		return err
	}
	c.push(&jsonrpc.Request{Method: method, Params: raw})
	return nil
}

// transport hands a conn to [mcp.Server.Connect] or [mcp.Client.Connect].
type transport struct{ c *conn }

func (t transport) Connect(context.Context) (mcp.Connection, error) { return t.c, nil }

// toParams and fromParams convert between MCP's raw params and the object
// mcp/message flattens them into. MCP params are always objects.
func toParams(raw json.RawMessage) (map[string]jsontext.Value, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var params map[string]jsontext.Value
	if err := jsonv2.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("acpmcp: MCP params must be an object: %w", err)
	}
	return params, nil
}

func fromParams(params map[string]jsontext.Value) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	return jsonv2.Marshal(params)
}

// toWireError turns the peer's error into the MCP error of a response.
func toWireError(err error) error {
	if err == nil {
		return nil
	}
	if re, ok := errors.AsType[*acp.RequestError](err); ok {
		wire := &jsonrpc.Error{Code: int64(re.Code), Message: re.Message}
		if re.Data != nil {
			wire.Data, _ = jsonv2.Marshal(re.Data)
		}
		return wire
	}
	return &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: err.Error()}
}

// toRequestError turns the local side's MCP error into the error of the
// peer's mcp/message call, keeping its code.
func toRequestError(err error) error {
	if wire, ok := errors.AsType[*jsonrpc.Error](err); ok {
		re := &acp.RequestError{Code: acp.ErrorCode(wire.Code), Message: wire.Message}
		if len(wire.Data) > 0 {
			re.Data = jsontext.Value(wire.Data)
		}
		return re
	}
	return acp.ErrInternalError(err.Error())
}
