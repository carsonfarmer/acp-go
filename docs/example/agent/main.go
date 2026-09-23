// Command agent is a complete ACP agent over stdio.
//
// It builds on the echo example with the pieces a real agent needs: session
// state and turn cancellation through an embedded acp1.SessionManager,
// session modes (modes.go), a plan and tool calls streamed with
// acp1.SessionStream (tools.go), a command run in the client's terminal, a
// file diff, a permission request before a destructive tool call, a typed
// extension method through acp.ExtRouter, and request logging through
// middleware.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"sync"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acp1"
)

// session holds the state the agent keeps per ACP session.
type session struct {
	cwd string

	mu   sync.Mutex
	mode acp1.SessionModeID // changed by session/set_mode while a turn may run
}

// exampleAgent embeds a SessionManager, which supplies NewSession and Cancel
// plus the optional session/load, session/list and session/delete handlers,
// and an ExtRouter, which serves the extension methods registered on it.
// It needs no credentials, so it leaves out Authenticate.
type exampleAgent struct {
	*acp1.SessionManager[*session]
	acp.ExtRouter
	client *acp1.AgentSideConnection

	// terminal is whether the client runs commands for the agent, from its
	// initialize request. Each connection has its own agent, so its own copy.
	terminal bool
}

// Extension methods start with an underscore and a domain the agent owns.
const pingMethod = "_example.com/ping"

type pingParams struct {
	Message string `json:"message"`
}

type pingResult struct {
	Reply string `json:"reply"`
}

func (a *exampleAgent) ping(_ context.Context, params *pingParams) (*pingResult, error) {
	return &pingResult{Reply: "pong: " + params.Message}, nil
}

func (a *exampleAgent) Initialize(_ context.Context, params *acp1.InitializeRequest) (*acp1.InitializeResponse, error) {
	if caps := params.ClientCapabilities; caps != nil && caps.Terminal != nil {
		a.terminal = *caps.Terminal
	}
	// CapabilitiesOf advertises exactly the optional methods implemented.
	return &acp1.InitializeResponse{
		ProtocolVersion:   acp1.ProtocolVersion,
		AgentCapabilities: acp1.CapabilitiesOf(a),
		AgentInfo:         &acp1.Implementation{Name: "example-agent", Version: "0.1.0"},
	}, nil
}

func (a *exampleAgent) Prompt(ctx context.Context, params *acp1.PromptRequest) (*acp1.PromptResponse, error) {
	sess, ok := a.Session(params.SessionID)
	if !ok {
		return nil, acp.ErrResourceNotFound(fmt.Sprintf("session %s", params.SessionID))
	}

	// The embedded manager's Cancel cancels this context.
	ctx, done, err := a.BeginTurn(ctx, params.SessionID)
	if err != nil {
		return nil, err // a second prompt while this session's turn runs
	}
	defer done()

	// Texts skips images, resources and other non-text blocks.
	prompt := strings.Join(slices.Collect(acp1.Texts(params.Prompt)), "")
	if err := a.runTurn(ctx, params.SessionID, sess, prompt); err != nil {
		if context.Cause(ctx) == acp.ErrTurnCancelled {
			return &acp1.PromptResponse{StopReason: acp1.StopReasonCancelled}, nil
		}
		return nil, err
	}
	return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
}

func main() {
	manager := acp1.NewSessionManager(
		acp1.NewMemoryStore[*session](),
		func(_ context.Context, params *acp1.NewSessionRequest) (acp1.SessionID, *session, error) {
			return acp1.GenerateSessionID(), &session{cwd: params.Cwd, mode: askMode}, nil
		},
	)

	// Stdout carries the protocol, so logs go to stderr.
	logger := log.New(os.Stderr, "", log.LstdFlags)
	conn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		a := &exampleAgent{SessionManager: manager, client: c}
		a.HandleExt(pingMethod, a.ping)
		return a
	}, os.Stdin, os.Stdout,
		acp.WithMiddleware(acp.LoggingMiddleware(logger.Printf)),
		acp.WithErrorHandler(func(err error) { logger.Printf("acp: %v", err) }),
	)

	if err := conn.Start(context.Background()); err != nil {
		logger.Printf("connection error: %v", err)
		os.Exit(1)
	}
}
