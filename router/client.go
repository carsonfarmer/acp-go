package router

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os/exec"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
	"github.com/ironpark/go-acp/acpv2"
	"github.com/ironpark/go-acp/internal/acpconn"
)

// ClientConnector spawns an agent and connects with the highest protocol
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
// request, capabilities included. That costs a second process start for
// v1-only agents.
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

// Agent is an initialized agent process speaking the negotiated version.
// Exactly one of V1 and V2 is set, with its initialize response.
type Agent struct {
	V1     *acpv1.AgentProcess
	V1Init *acpv1.InitializeResponse
	V2     *acpv2.AgentProcess
	V2Init *acpv2.InitializeResponse
}

// agentProcess is what [acpv1.AgentProcess] and [acpv2.AgentProcess] share.
type agentProcess interface {
	acp.ExtCaller
	ExtNotification(ctx context.Context, method string, params any) error
	Close() error
	Wait() error
	Done() <-chan struct{}
}

var _ acp.ExtCaller = (*Agent)(nil)

func (a *Agent) process() agentProcess {
	if a.V2 != nil {
		return a.V2
	}
	return a.V1
}

// Close shuts the connection down, whichever version it speaks.
func (a *Agent) Close() error { return a.process().Close() }

// Wait blocks until the agent process has exited and the connection has
// stopped, as the version's AgentProcess.Wait describes.
func (a *Agent) Wait() error { return a.process().Wait() }

// Done is closed once the connection stops.
func (a *Agent) Done() <-chan struct{} { return a.process().Done() }

// ExtMethod sends a request outside the spec, so [acp.CallExt] works without
// knowing the negotiated version.
func (a *Agent) ExtMethod(ctx context.Context, method string, params any) (jsontext.Value, error) {
	return a.process().ExtMethod(ctx, method, params)
}

// ExtNotification sends a notification outside the spec.
func (a *Agent) ExtNotification(ctx context.Context, method string, params any) error {
	return a.process().ExtNotification(ctx, method, params)
}

// ErrNoCommonVersion reports an agent whose protocol version is not one the
// connector was configured for.
var ErrNoCommonVersion = errors.New("router: agent supports no configured protocol version")

// Spawn starts the agent with a command from newCmd and initializes it. newCmd
// is called again when a v2 attempt falls back to v1, since an exec.Cmd runs
// only once. opts configure whichever connection is kept.
func (c *ClientConnector) Spawn(ctx context.Context, newCmd func() *exec.Cmd, opts ...acp.Option) (*Agent, error) {
	switch {
	case c.v2 != nil:
		agent, negotiated, err := c.spawnV2(ctx, newCmd, opts)
		if err != nil || agent != nil {
			return agent, err
		}
		if negotiated != acpv1.ProtocolVersion || c.v1 == nil {
			return nil, fmt.Errorf("%w: agent answered protocolVersion %d", ErrNoCommonVersion, negotiated)
		}
		return c.spawnV1(ctx, newCmd, opts)
	case c.v1 != nil:
		return c.spawnV1(ctx, newCmd, opts)
	}
	return nil, errors.New("router: no client protocol version configured")
}

// spawnV2 initializes with v2. It returns the agent when v2 was accepted, or
// the version the agent answered after shutting the process down.
func (c *ClientConnector) spawnV2(ctx context.Context, newCmd func() *exec.Cmd, opts []acp.Option) (*Agent, int, error) {
	proc, err := acpv2.SpawnAgent(ctx, newCmd(), c.v2.newClient, opts...)
	if err != nil {
		return nil, 0, err
	}
	// Read the result raw: a v1 agent answers with a v1-shaped response that
	// need not decode as a v2 one.
	raw, err := proc.ExtMethod(ctx, initializeMethod, &c.v2.init)
	if err != nil {
		shutdown(proc)
		if acp.IsCode(err, acp.ErrorCodeInvalidParams) || acp.IsCode(err, acp.ErrorCodeInvalidRequest) {
			// A strict v1 agent may reject the v2 request shape outright
			// rather than answer protocolVersion 1.
			return nil, acpv1.ProtocolVersion, nil
		}
		return nil, 0, fmt.Errorf("initialize: %w", err)
	}
	version, _ := protocolVersionOf(raw) // 0 if missing or malformed: no common version
	if version != acpv2.ProtocolVersion {
		shutdown(proc)
		return nil, int(version), nil
	}
	init, err := acpconn.DecodeResult[acpv2.InitializeResponse](raw)
	if err != nil {
		shutdown(proc)
		return nil, 0, fmt.Errorf("initialize: %w", err)
	}
	return &Agent{V2: proc, V2Init: init}, 0, nil
}

func (c *ClientConnector) spawnV1(ctx context.Context, newCmd func() *exec.Cmd, opts []acp.Option) (*Agent, error) {
	proc, err := acpv1.SpawnAgent(ctx, newCmd(), c.v1.newClient, opts...)
	if err != nil {
		return nil, err
	}
	init, err := proc.Initialize(ctx, &c.v1.init)
	if err != nil {
		shutdown(proc)
		return nil, fmt.Errorf("initialize: %w", err)
	}
	if init.ProtocolVersion != acpv1.ProtocolVersion {
		shutdown(proc)
		return nil, fmt.Errorf("%w: agent answered protocolVersion %d", ErrNoCommonVersion, init.ProtocolVersion)
	}
	return &Agent{V1: proc, V1Init: init}, nil
}

// shutdown closes an agent process's connection and waits for it to exit.
func shutdown(proc agentProcess) {
	_ = proc.Close()
	_ = proc.Wait()
}
