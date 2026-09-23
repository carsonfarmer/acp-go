package router

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
	"github.com/ironpark/go-acp/acpv2"
	"github.com/ironpark/go-acp/internal/acpconn"
)

// ClientConnector spawns or connects to an agent with the highest protocol
// version both sides support, the client-side counterpart of
// [ProtocolRouter]:
//
//	agent, err := router.NewClient().
//		WithV1(newV1Client, &acpv1.InitializeRequest{ClientCapabilities: v1Caps}).
//		WithV2(newV2Client, &acpv2.InitializeRequest{Info: info}).
//		Spawn(ctx, func() *exec.Cmd { return exec.Command("my-agent") })
//	defer agent.Close()
//	switch {
//	case agent.V2 != nil: // drive the v2 session API
//	case agent.V1 != nil: // drive the v1 session API
//	}
//
// With both versions configured it initializes with v2 first. An agent that
// answers protocolVersion 1, or rejects the v2 request as invalid, is shut
// down and started again with v1, so each version sees its own initialize
// request, capabilities included. That costs a second process start, or a
// second connection with [ClientConnector.Connect], for v1-only agents.
type ClientConnector struct {
	v1 *clientV1
	v2 *clientV2
}

type clientV1 struct {
	newClient func(*acpv1.ClientSideConnection) acpv1.Client
	init      acpv1.InitializeRequest
}

type clientV2 struct {
	newClient func(*acpv2.ClientSideConnection) acpv2.Client
	init      acpv2.InitializeRequest
}

// NewClient creates a connector with no protocol versions configured.
func NewClient() *ClientConnector { return &ClientConnector{} }

// WithV1 configures the ACP v1 client and the initialize request it sends.
// The request's ProtocolVersion is set for you; init may be nil.
func (c *ClientConnector) WithV1(newClient func(*acpv1.ClientSideConnection) acpv1.Client, init *acpv1.InitializeRequest) *ClientConnector {
	c.v1 = &clientV1{newClient: newClient}
	if init != nil {
		c.v1.init = *init
	}
	c.v1.init.ProtocolVersion = acpv1.ProtocolVersion
	return c
}

// WithV2 configures the draft ACP v2 client and the initialize request it
// sends. The request's ProtocolVersion is set for you; init may be nil.
func (c *ClientConnector) WithV2(newClient func(*acpv2.ClientSideConnection) acpv2.Client, init *acpv2.InitializeRequest) *ClientConnector {
	c.v2 = &clientV2{newClient: newClient}
	if init != nil {
		c.v2.init = *init
	}
	c.v2.init.ProtocolVersion = acpv2.ProtocolVersion
	return c
}

// Agent is an initialized agent speaking the negotiated version. Exactly one
// of V1 and V2 is set, with its initialize response; the embedded Connection
// is the same agent for code that does not depend on the version.
type Agent struct {
	Connection
	V1     *acpv1.RemoteAgent
	V1Init *acpv1.InitializeResponse
	V2     *acpv2.RemoteAgent
	V2Init *acpv2.InitializeResponse
}

// Connection is what [acpv1.RemoteAgent] and [acpv2.RemoteAgent] share.
type Connection interface {
	acp.Conn
	// Wait blocks until the connection has stopped, and a spawned agent's
	// process has exited.
	Wait() error
}

// ErrNoCommonVersion reports an agent whose protocol version is not one the
// connector was configured for.
var ErrNoCommonVersion = errors.New("router: agent supports no configured protocol version")

// Spawn starts the agent with a command from newCmd and initializes it. newCmd
// is called again when a v2 attempt falls back to v1, since an exec.Cmd runs
// only once. opts configure whichever connection is kept.
func (c *ClientConnector) Spawn(ctx context.Context, newCmd func() *exec.Cmd, opts ...acp.Option) (*Agent, error) {
	return c.negotiate(ctx,
		func() (*acpv1.RemoteAgent, error) { return acpv1.SpawnAgent(ctx, newCmd(), c.v1.newClient, opts...) },
		func() (*acpv2.RemoteAgent, error) { return acpv2.SpawnAgent(ctx, newCmd(), c.v2.newClient, opts...) })
}

// Connect is Spawn for an agent reached over a transport, such as Streamable
// HTTP or WebSocket:
//
//	agent, err := router.NewClient().WithV1(…).WithV2(…).
//		Connect(ctx, func(ctx context.Context) (acp.Transport, error) {
//			return acp.NewHTTPClientTransport("https://host/acp"), nil
//		})
//
// dial is called for each attempt, since a transport carries one connection;
// a v2 attempt that falls back to v1 closes its transport first.
func (c *ClientConnector) Connect(ctx context.Context, dial func(context.Context) (acp.Transport, error), opts ...acp.Option) (*Agent, error) {
	return c.negotiate(ctx,
		func() (*acpv1.RemoteAgent, error) {
			t, err := dial(ctx)
			if err != nil {
				return nil, err
			}
			return acpv1.ConnectAgent(ctx, t, c.v1.newClient, opts...), nil
		},
		func() (*acpv2.RemoteAgent, error) {
			t, err := dial(ctx)
			if err != nil {
				return nil, err
			}
			return acpv2.ConnectAgent(ctx, t, c.v2.newClient, opts...), nil
		})
}

// negotiate initializes with v2 when configured, falling back to a fresh v1
// connection when the agent answers protocolVersion 1.
func (c *ClientConnector) negotiate(ctx context.Context, startV1 func() (*acpv1.RemoteAgent, error), startV2 func() (*acpv2.RemoteAgent, error)) (*Agent, error) {
	switch {
	case c.v2 != nil:
		agent, negotiated, err := c.initializeV2(ctx, startV2)
		if err != nil || agent != nil {
			return agent, err
		}
		if negotiated != acpv1.ProtocolVersion || c.v1 == nil {
			return nil, fmt.Errorf("%w: agent answered protocolVersion %d", ErrNoCommonVersion, negotiated)
		}
		return c.initializeV1(ctx, startV1)
	case c.v1 != nil:
		return c.initializeV1(ctx, startV1)
	}
	return nil, errors.New("router: no client protocol version configured")
}

// initializeV2 initializes with v2. It returns the agent when v2 was
// accepted, or the version the agent answered after shutting it down.
func (c *ClientConnector) initializeV2(ctx context.Context, start func() (*acpv2.RemoteAgent, error)) (*Agent, int, error) {
	remote, err := start()
	if err != nil {
		return nil, 0, err
	}
	// Read the result raw: a v1 agent answers with a v1-shaped response that
	// need not decode as a v2 one.
	raw, err := remote.ExtMethod(ctx, initializeMethod, &c.v2.init)
	if err != nil {
		shutdown(remote)
		if acp.IsCode(err, acp.ErrorCodeInvalidParams) || acp.IsCode(err, acp.ErrorCodeInvalidRequest) {
			// A strict v1 agent may reject the v2 request shape outright
			// rather than answer protocolVersion 1.
			return nil, acpv1.ProtocolVersion, nil
		}
		return nil, 0, fmt.Errorf("initialize: %w", err)
	}
	version, _ := protocolVersionOf(raw) // 0 if missing or malformed: no common version
	if version != acpv2.ProtocolVersion {
		shutdown(remote)
		return nil, int(version), nil
	}
	init, err := acpconn.DecodeResult[acpv2.InitializeResponse](raw)
	if err != nil {
		shutdown(remote)
		return nil, 0, fmt.Errorf("initialize: %w", err)
	}
	return &Agent{Connection: remote, V2: remote, V2Init: init}, 0, nil
}

func (c *ClientConnector) initializeV1(ctx context.Context, start func() (*acpv1.RemoteAgent, error)) (*Agent, error) {
	remote, err := start()
	if err != nil {
		return nil, err
	}
	init, err := remote.Initialize(ctx, &c.v1.init)
	if err != nil {
		shutdown(remote)
		return nil, fmt.Errorf("initialize: %w", err)
	}
	if init.ProtocolVersion != acpv1.ProtocolVersion {
		shutdown(remote)
		return nil, fmt.Errorf("%w: agent answered protocolVersion %d", ErrNoCommonVersion, init.ProtocolVersion)
	}
	return &Agent{Connection: remote, V1: remote, V1Init: init}, nil
}

// shutdown closes a connection and waits for it, and any process, to end.
func shutdown(conn Connection) {
	_ = conn.Close()
	_ = conn.Wait()
}
