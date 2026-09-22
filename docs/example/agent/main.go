// Command agent is a minimal ACP agent over stdio.
//
// It shows the pieces an agent implementation needs: the required Agent
// methods, session state through an embedded acpv1.SessionManager, streaming
// updates with acpv1.SessionStream, and a permission request before a
// destructive tool call.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// session holds the state the agent keeps per ACP session. Prompt and Cancel
// run on different goroutines, so the turn's cancel func is guarded.
type session struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

func (s *session) startTurn(cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel = cancel
}

func (s *session) cancelTurn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

// exampleAgent embeds a SessionManager, which supplies NewSession plus the
// optional session/load, session/list and session/delete handlers.
type exampleAgent struct {
	*acpv1.SessionManager[*session]
	client acpv1.Client
}

func (a *exampleAgent) Initialize(_ context.Context, _ *acpv1.InitializeRequest) (*acpv1.InitializeResponse, error) {
	// The embedded SessionManager implements session/load, list and delete;
	// CapabilitiesOf advertises exactly what is implemented.
	return &acpv1.InitializeResponse{
		ProtocolVersion:   acpv1.ProtocolVersion,
		AgentCapabilities: acpv1.CapabilitiesOf(a),
		AgentInfo:         &schema.Implementation{Name: "example-agent", Version: "0.1.0"},
	}, nil
}

func (a *exampleAgent) Authenticate(_ context.Context, _ *acpv1.AuthenticateRequest) (*acpv1.AuthenticateResponse, error) {
	return &acpv1.AuthenticateResponse{}, nil
}

func (a *exampleAgent) Prompt(ctx context.Context, params *acpv1.PromptRequest) (*acpv1.PromptResponse, error) {
	current, ok := a.Session(params.SessionID)
	if !ok {
		return nil, acp.ErrResourceNotFound(fmt.Sprintf("session %s", params.SessionID))
	}

	// One turn at a time per session: replace the previous turn's cancel func.
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	current.startTurn(cancel)

	if err := a.runTurn(turnCtx, params.SessionID); err != nil {
		if turnCtx.Err() != nil {
			return &acpv1.PromptResponse{StopReason: schema.StopReasonCancelled}, nil
		}
		return nil, err
	}
	return &acpv1.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil
}

// Cancel ends the turn in progress. Prompt then returns the cancelled stop
// reason rather than an error.
func (a *exampleAgent) Cancel(_ context.Context, params *acpv1.CancelNotification) error {
	if current, ok := a.Session(params.SessionID); ok {
		current.cancelTurn()
	}
	return nil
}

func (a *exampleAgent) runTurn(ctx context.Context, sessionID acpv1.SessionID) error {
	stream := acpv1.NewSessionStream(a.client, sessionID)

	if err := stream.SendText(ctx, "Let me read the project first."); err != nil {
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
	if err := stream.CompleteToolCall(ctx, read, schema.NewToolCallContent(schema.ToolCallContentContent{
		Content: schema.NewContentBlock(schema.ContentBlockText{Text: "# My Project"}),
	})); err != nil {
		return err
	}

	edit := acpv1.ToolCallID("call_2")
	if err := stream.StartToolCall(ctx, edit, "Modifying configuration", schema.ToolKindEdit); err != nil {
		return err
	}

	// Editing a file is destructive, so ask the user first.
	pending := schema.ToolCallStatusPending
	kind := schema.ToolKindEdit
	title := "Modifying configuration"
	permission, err := a.client.RequestPermission(ctx, &acpv1.RequestPermissionRequest{
		SessionID: sessionID,
		ToolCall: schema.ToolCallUpdate{
			ToolCallID: edit,
			Title:      &title,
			Kind:       &kind,
			Status:     &pending,
			Locations:  []acpv1.ToolCallLocation{{Path: "/project/config.json"}},
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
		func(_ context.Context, _ *acpv1.NewSessionRequest) (acpv1.SessionID, *session, error) {
			return acpv1.GenerateSessionID(), &session{}, nil
		},
	)

	// Stdout carries the protocol, so logs go to stderr.
	logger := log.New(os.Stderr, "", log.LstdFlags)
	conn := acpv1.NewAgentSideConnection(func(c *acpv1.AgentSideConnection) acpv1.Agent {
		return &exampleAgent{SessionManager: manager, client: c}
	}, os.Stdin, os.Stdout,
		acp.WithErrorHandler(func(err error) { logger.Printf("acp: %v", err) }),
	)

	if err := conn.Start(context.Background()); err != nil {
		logger.Printf("connection error: %v", err)
		os.Exit(1)
	}
}
