// Command agent is a complete ACP agent over stdio.
//
// It builds on the echo example with the pieces a real agent needs: session
// state and turn cancellation through an embedded acpv1.SessionManager,
// streamed text and tool calls with acpv1.SessionStream, a permission request
// before a destructive tool call, a typed extension method through
// acp.ExtRouter, and request logging through middleware.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// session holds the state the agent keeps per ACP session.
type session struct {
	cwd string
}

// exampleAgent embeds a SessionManager, which supplies NewSession and Cancel
// plus the optional session/load, session/list and session/delete handlers,
// and an ExtRouter, which serves the extension methods registered on it.
// It needs no credentials, so it leaves out Authenticate.
type exampleAgent struct {
	*acpv1.SessionManager[*session]
	acp.ExtRouter
	client acpv1.Client
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

func (a *exampleAgent) Initialize(_ context.Context, _ *acpv1.InitializeRequest) (*acpv1.InitializeResponse, error) {
	// CapabilitiesOf advertises exactly the optional methods implemented.
	return &acpv1.InitializeResponse{
		ProtocolVersion:   acpv1.ProtocolVersion,
		AgentCapabilities: acpv1.CapabilitiesOf(a),
		AgentInfo:         &schema.Implementation{Name: "example-agent", Version: "0.1.0"},
	}, nil
}

func (a *exampleAgent) Prompt(ctx context.Context, params *acpv1.PromptRequest) (*acpv1.PromptResponse, error) {
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
	prompt := strings.Join(slices.Collect(acpv1.Texts(params.Prompt)), "")
	if err := a.runTurn(ctx, params.SessionID, sess, prompt); err != nil {
		if context.Cause(ctx) == acp.ErrTurnCancelled {
			return &acpv1.PromptResponse{StopReason: schema.StopReasonCancelled}, nil
		}
		return nil, err
	}
	return &acpv1.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil
}

func (a *exampleAgent) runTurn(ctx context.Context, sessionID acpv1.SessionID, sess *session, prompt string) error {
	stream := acpv1.NewSessionStream(a.client, sessionID)

	if err := stream.SendText(ctx, fmt.Sprintf("You said %q. Let me read the project first.", prompt)); err != nil {
		return err
	}
	if err := pause(ctx); err != nil {
		return err
	}

	read := acpv1.ToolCallID("call_1")
	if err := stream.StartToolCall(ctx, read, "Reading project files", schema.ToolKindRead); err != nil {
		return err
	}
	if err := pause(ctx); err != nil {
		return err
	}
	if err := stream.CompleteToolCall(ctx, read, acpv1.ToolText("# My Project")); err != nil {
		return err
	}

	edit := acpv1.ToolCallID("call_2")
	if err := stream.StartToolCall(ctx, edit, "Modifying configuration", schema.ToolKindEdit); err != nil {
		return err
	}

	// Editing a file is destructive, so ask the user first.
	permission, err := a.client.RequestPermission(ctx, &acpv1.RequestPermissionRequest{
		SessionID: sessionID,
		ToolCall: schema.ToolCallUpdate{
			ToolCallID: edit,
			Title:      new("Modifying configuration"),
			Kind:       new(schema.ToolKindEdit),
			Status:     new(schema.ToolCallStatusPending),
			Locations:  []acpv1.ToolCallLocation{{Path: filepath.Join(sess.cwd, "config.json")}},
		},
		Options: []schema.PermissionOption{
			{OptionID: "allow", Name: "Allow this change", Kind: schema.PermissionOptionKindAllowOnce},
			{OptionID: "reject", Name: "Skip this change", Kind: schema.PermissionOptionKindRejectOnce},
		},
	})
	if err != nil {
		return err
	}

	switch outcome := permission.Outcome.Variant().(type) {
	case schema.RequestPermissionOutcomeSelected:
		if outcome.OptionID != "allow" {
			if err := stream.FailToolCall(ctx, edit); err != nil {
				return err
			}
			return stream.SendText(ctx, " Skipping the configuration update.")
		}
		if err := stream.CompleteToolCall(ctx, edit); err != nil {
			return err
		}
		return stream.SendText(ctx, " Configuration updated.")
	default:
		// Cancelled, or an outcome added after this example was written.
		return stream.FailToolCall(ctx, edit)
	}
}

// pause stands in for real work and returns early when the turn is cancelled.
func pause(ctx context.Context) error {
	select {
	case <-time.After(500 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func main() {
	manager := acpv1.NewSessionManager(
		acpv1.NewMemoryStore[*session](),
		func(_ context.Context, params *acpv1.NewSessionRequest) (acpv1.SessionID, *session, error) {
			return acpv1.GenerateSessionID(), &session{cwd: params.Cwd}, nil
		},
	)

	// Stdout carries the protocol, so logs go to stderr.
	logger := log.New(os.Stderr, "", log.LstdFlags)
	conn := acpv1.NewAgentSideConnection(func(c *acpv1.AgentSideConnection) acpv1.Agent {
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
